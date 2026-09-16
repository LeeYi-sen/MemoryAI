package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

func meshProposalReplayIdentityFromRequest(req MeshRequest) (meshProposalIdentity, string, string, error) {
	f := newFrame()
	f.Vars["memory_id"] = strings.TrimSpace(req.MemoryID)
	f.Vars["origin_node"] = strings.TrimSpace(req.OriginNode)
	f.Vars["proposal_digest"] = strings.TrimSpace(req.ProposalDigest)
	f.Vars["revision"] = fmt.Sprint(req.Revision)
	f.Lists["tags"] = append([]string(nil), req.Tags...)
	ev := newPhysicalEvent("mesh.shared.proposal", f.Vars["memory_id"], f.Vars)
	return meshProposalIdentityFromFrame(f, ev)
}

// acknowledgeMeshProposalReplay removes a completed replay receipt only after
// the caller explicitly acknowledges that it no longer needs the stored
// result. The durable per-slot high-watermark is retained, so the acknowledged
// proposal remains fenced against re-execution after receipt collection and
// after process restart.
func acknowledgeMeshProposalReplay(e *Engine, req MeshRequest) error {
	identity, slot, requestID, err := meshProposalReplayIdentityFromRequest(req)
	if err != nil {
		return err
	}
	root, err := meshProposalReplayRoot(e)
	if err != nil {
		return err
	}
	entry, _, found, err := loadMeshProposalReplayEntry(root, requestID)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("mesh proposal replay receipt not found")
	}
	if entry.Slot != slot || entry.Revision != identity.Revision || entry.RequestID != requestID {
		return errors.New("mesh proposal replay ack identity collision")
	}
	if entry.State != meshProposalReplayDone {
		return fmt.Errorf("mesh proposal replay receipt not completed: %s", entry.State)
	}

	latestRequestID, latestRevision, _, slotFound, err := loadMeshProposalSlot(root, slot)
	if err != nil {
		return err
	}
	if !slotFound || latestRevision < entry.Revision {
		return errors.New("mesh proposal replay ack missing durable slot fence")
	}
	if latestRevision == entry.Revision && latestRequestID != entry.RequestID {
		return errors.New("mesh proposal replay ack slot identity drift")
	}

	if err := root.deleteExplicitMemoryBounded(meshProposalReceiptID(requestID)); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("mesh proposal replay receipt disappeared before ack persistence")
		}
		return err
	}
	if err := root.persistAll(); err != nil {
		return fmt.Errorf("persist mesh proposal replay acknowledgement: %w", err)
	}
	return nil
}

func (m *meshRuntime) acknowledgeSharedProposal(req MeshRequest) MeshResponse {
	if m == nil {
		return MeshResponse{OK: false, Error: "mesh runtime unavailable"}
	}
	m.mu.RLock()
	e := m.engine
	m.mu.RUnlock()
	if e == nil {
		return MeshResponse{OK: false, Error: "sovereign engine unavailable"}
	}
	if err := acknowledgeMeshProposalReplay(e, req); err != nil {
		return MeshResponse{OK: false, Error: err.Error()}
	}
	return MeshResponse{OK: true, Status: "acknowledged"}
}

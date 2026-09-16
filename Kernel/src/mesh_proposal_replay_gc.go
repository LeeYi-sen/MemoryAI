package main

import (
	"errors"
	"io"
)

// reclaimOneSupersededMeshProposalReplayReceipt frees at most one replay
// receipt, and only when the durable slot high-watermark proves that the
// completed receipt has been superseded by a strictly newer proposal.
//
// The slot Memory is deliberately retained. After the receipt is removed, a
// retry of the old proposal is still rejected by prepareMeshProposalReplay as
// stale, including after process restart. Executing/indeterminate receipts are
// never eligible for collection.
func reclaimOneSupersededMeshProposalReplayReceipt(e *Engine, ids []string) (bool, error) {
	root, err := meshProposalReplayRoot(e)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		_, memory, err := root.resolveLocalFabricMemory(id)
		if errors.Is(err, io.EOF) {
			continue
		}
		if err != nil {
			return false, err
		}
		entry, err := decodeMeshProposalReplayEntry(memory)
		if err != nil {
			return false, err
		}
		if entry.State != meshProposalReplayDone {
			continue
		}
		latestRequestID, latestRevision, _, found, err := loadMeshProposalSlot(root, entry.Slot)
		if err != nil {
			return false, err
		}
		if !found || latestRevision <= entry.Revision || latestRequestID == entry.RequestID {
			continue
		}
		if err := root.deleteExplicitMemoryBounded(id); err != nil {
			return false, err
		}
		if err := root.persistAll(); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

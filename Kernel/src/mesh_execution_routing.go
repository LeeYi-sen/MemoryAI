package main

import (
	"crypto/sha256"
	"errors"
	"sort"
)

// routeExecution chooses a physically reachable node for one explicit
// executable Memory ID. It uses no semantic score, relevance, goal, utility or
// policy inference; target meaning and whether this operation should happen are
// supplied by executable Memory/VM state outside this physical routing primitive.
func (m *meshRuntime) routeExecution(id string, vars map[string]string, preferLocal bool) (string, *Frame, error) {
	if m == nil || m.role != "sovereign" {
		return "", nil, errors.New("physical execution routing requires sovereign role")
	}
	m.mu.RLock()
	nodes := make([]MeshNode, 0, len(m.directory))
	for _, n := range m.directory {
		nodes = append(nodes, n)
	}
	self := m.nodeID
	e := m.engine
	m.mu.RUnlock()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	if preferLocal && e != nil {
		res := m.serveLocalExecution(id, vars)
		if res.OK {
			return self, res.Frame, nil
		}
	}
	if len(nodes) == 0 {
		return "", nil, errors.New("no mesh nodes available")
	}
	// Deterministic physical sharding only; no semantic score is computed.
	h := sha256.Sum256([]byte(id))
	start := int(h[0]) % len(nodes)
	for i := 0; i < len(nodes); i++ {
		n := nodes[(start+i)%len(nodes)]
		if n.ID == self {
			if e == nil {
				continue
			}
			res := m.serveLocalExecution(id, vars)
			if res.OK {
				return n.ID, res.Frame, nil
			}
			continue
		}
		if n.Endpoint == "" {
			continue
		}
		grant, err := issueMeshGrant("route_execute", id, self, n.ID, meshGrantTTL)
		if err != nil {
			return "", nil, err
		}
		res, err := m.rpc(n.Endpoint, MeshRequest{Op: "structure_run", MemoryID: id, Vars: vars, Grant: grant})
		if err == nil && res.OK {
			return n.ID, res.Frame, nil
		}
	}
	return "", nil, errors.New("no physically reachable node could execute structure")
}

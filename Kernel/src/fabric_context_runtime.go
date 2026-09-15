package main

import "sync"

// fabricRootRegistry is a physical topology hint only. It links passive storage
// Engines back to the core Engine that mounted them so runtime plumbing can keep
// one local Fabric execution context while persistence still targets the true
// physical owner. No semantic or cognitive state is stored here.
var fabricRootRegistry sync.Map // map[*Engine]*Engine

func rememberFabricOwner(root, member *Engine) {
	if root == nil || member == nil {
		return
	}
	if root.manifest.Role != "core" {
		if v, ok := fabricRootRegistry.Load(root); ok {
			root = v.(*Engine)
		}
	}
	if root == nil || root.manifest.Role != "core" {
		return
	}
	fabricRootRegistry.Store(root, root)
	fabricRootRegistry.Store(member, root)
}

func fabricRootFor(e *Engine) *Engine {
	if e == nil {
		return nil
	}
	if origin, ok := speculativeOriginEngine(e); ok && origin != nil {
		e = origin
	}
	if v, ok := fabricRootRegistry.Load(e); ok {
		return v.(*Engine)
	}
	if e.manifest.Role == "core" {
		fabricRootRegistry.Store(e, e)
		return e
	}
	return e
}

func forgetFabricOwner(e *Engine) {
	if e != nil {
		fabricRootRegistry.Delete(e)
	}
}

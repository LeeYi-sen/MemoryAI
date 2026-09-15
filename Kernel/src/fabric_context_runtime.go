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

func engineStoreOpen(e *Engine) bool {
	if e == nil {
		return false
	}
	e.dataMu.RLock()
	open := e.store != nil
	e.dataMu.RUnlock()
	return open
}

func rootStillMountsMember(root, member *Engine) bool {
	if root == nil || member == nil || root == member {
		return root == member
	}
	root.spaceMu.RLock()
	defer root.spaceMu.RUnlock()
	for _, mounted := range root.spaces {
		if mounted == member {
			return true
		}
	}
	return false
}

func fabricRootFor(e *Engine) *Engine {
	if e == nil {
		return nil
	}
	if origin, ok := speculativeOriginEngine(e); ok && origin != nil {
		e = origin
	}
	if v, ok := fabricRootRegistry.Load(e); ok {
		root := v.(*Engine)
		// Registry entries are hints, never authority. A closed core or a shard
		// that is no longer mounted must not retain the old execution context.
		if !engineStoreOpen(root) || !rootStillMountsMember(root, e) {
			fabricRootRegistry.Delete(e)
			if root == e {
				fabricRootRegistry.Delete(root)
			}
		} else {
			return root
		}
	}
	if e.manifest.Role == "core" && engineStoreOpen(e) {
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

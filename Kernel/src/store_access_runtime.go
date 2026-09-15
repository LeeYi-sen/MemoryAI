package main

// These helpers are the only live Engine -> IndexedStore access path. The
// underlying body file is atomically replaced during persistence, so readers
// take storeMu.RLock while finalizePersistedBody swaps/closes under storeMu.Lock.
// This lock protects file-descriptor lifetime only; it carries no Memory policy.

func (e *Engine) storeGetID(id string) (*Memory, error) {
	e.storeMu.RLock()
	defer e.storeMu.RUnlock()
	return e.store.GetID(id)
}

func (e *Engine) storeTagIDs(tag string) ([]string, error) {
	e.storeMu.RLock()
	defer e.storeMu.RUnlock()
	return e.store.TagIDs(tag)
}

func (e *Engine) storeAllIDs() ([]string, error) {
	e.storeMu.RLock()
	defer e.storeMu.RUnlock()
	return e.store.AllIDs()
}

func (e *Engine) storePhysicalFeatureIDs(feature string) ([]string, error) {
	e.storeMu.RLock()
	defer e.storeMu.RUnlock()
	return e.store.PhysicalFeatureIDs(feature)
}

func (e *Engine) storeHasPhysicalFeatureIndex() (bool, error) {
	e.storeMu.RLock()
	defer e.storeMu.RUnlock()
	return e.store.HasPhysicalFeatureIndex()
}

func (e *Engine) storeMemoryCount() int {
	e.storeMu.RLock()
	defer e.storeMu.RUnlock()
	if e.store == nil {
		return 0
	}
	return e.store.memoryCount
}

func (e *Engine) closeStore() {
	e.storeMu.Lock()
	defer e.storeMu.Unlock()
	if e.store != nil {
		e.store.Close()
		e.store = nil
	}
}

package main

import (
	"io"
	"sync"
)

// IndexedStore file descriptors are shared by the primary Engine and lazy
// speculative Engines. A lock embedded only in Engine would therefore not
// protect a shared store from being closed by another Engine. Guards are keyed
// by the physical IndexedStore instance so every user of the same descriptor
// coordinates through one lifetime boundary.
var indexedStoreLifetime sync.Map // map[*IndexedStore]*sync.RWMutex

func indexedStoreGuard(st *IndexedStore) *sync.RWMutex {
	if st == nil {
		return nil
	}
	if g, ok := indexedStoreLifetime.Load(st); ok {
		return g.(*sync.RWMutex)
	}
	g := &sync.RWMutex{}
	actual, _ := indexedStoreLifetime.LoadOrStore(st, g)
	return actual.(*sync.RWMutex)
}

// acquireStoreLifetimeLease pins the Engine's current physical store. The
// Engine data lock protects the store pointer while the shared guard is
// acquired; after that the guard alone prevents persistence from closing the
// descriptor until release is called.
func (e *Engine) acquireStoreLifetimeLease() (*IndexedStore, func(), error) {
	if e == nil {
		return nil, nil, io.EOF
	}
	e.dataMu.RLock()
	st := e.store
	if st == nil {
		e.dataMu.RUnlock()
		return nil, nil, io.EOF
	}
	g := indexedStoreGuard(st)
	g.RLock()
	e.dataMu.RUnlock()
	return st, g.RUnlock, nil
}

func (e *Engine) storeGetID(id string) (*Memory, error) {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, err
	}
	defer release()
	return st.GetID(id)
}

func (e *Engine) storeTagIDs(tag string) ([]string, error) {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, err
	}
	defer release()
	return st.TagIDs(tag)
}

func (e *Engine) storeAllIDs() ([]string, error) {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, err
	}
	defer release()
	return st.AllIDs()
}

func (e *Engine) storePhysicalFeatureIDs(feature string) ([]string, error) {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, err
	}
	defer release()
	return st.PhysicalFeatureIDs(feature)
}

func (e *Engine) storeHasPhysicalFeatureIndex() (bool, error) {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return false, err
	}
	defer release()
	return st.HasPhysicalFeatureIndex()
}

func (e *Engine) storeMemoryCount() int {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return 0
	}
	defer release()
	return st.memoryCount
}

// closeStore is used only when an Engine is no longer available to new work.
// The write guard waits for any in-flight physical reads before closing the
// descriptor.
func (e *Engine) closeStore() {
	if e == nil {
		return
	}
	e.dataMu.Lock()
	st := e.store
	if st == nil {
		e.dataMu.Unlock()
		return
	}
	g := indexedStoreGuard(st)
	g.Lock()
	e.store = nil
	_ = st.file.Close()
	g.Unlock()
	indexedStoreLifetime.Delete(st)
	e.dataMu.Unlock()
}

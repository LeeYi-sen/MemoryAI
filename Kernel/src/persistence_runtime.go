package main

import (
	"archive/zip"
	"fmt"
	"path/filepath"
)

// finalizePersistedBody rebinds an Engine to the newly renamed physical body.
// writeDetZip replaces the pathname atomically, so the prior IndexedStore file
// descriptor still points at the old inode until we explicitly reopen it.
//
// This is purely storage bookkeeping: no semantic merge or cognitive policy is
// performed here.
func finalizePersistedBody(e *Engine, out string) error {
	if e == nil {
		return fmt.Errorf("persist finalize requires engine")
	}
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	bodyAbs, err := filepath.Abs(e.bodyPath)
	if err != nil {
		return err
	}
	// saveBody may also be used as an export/copy operation. Only a write back to
	// the Engine's active body mutates live store state.
	if filepath.Clean(outAbs) != filepath.Clean(bodyAbs) {
		return nil
	}

	zr, err := zip.OpenReader(outAbs)
	if err != nil {
		return err
	}
	defer zr.Close()

	var mf Manifest
	if err = readJSONZip(&zr.Reader, "manifest.json", &mf); err != nil {
		return err
	}
	var g Genesis
	if err = readJSONZip(&zr.Reader, mf.GenesisPath, &g); err != nil {
		return err
	}
	st, err := openIndexedStore(outAbs, zr, mf.Store)
	if err != nil {
		return err
	}
	if err = validateIndexedStore(st); err != nil {
		st.Close()
		return err
	}

	e.dataMu.Lock()
	old := e.store
	e.store = st
	e.manifest = mf
	e.genesis = g
	e.newIDs = map[string]bool{}
	e.dirtyIDs = map[string]bool{}
	e.deletedIDs = map[string]bool{}
	e.tagAdded = map[string]map[string]bool{}
	e.tagRemoved = map[string]map[string]bool{}
	e.dirty = false
	e.dataMu.Unlock()

	if old != nil {
		old.Close()
	}
	// The persisted physical index is now authoritative. Rebuild only the small
	// mutable activation overlay; new-format stores take the non-scanning path.
	if e.manifest.Role == "core" {
		if err := globalActivationRuntime.Build(e); err != nil {
			return err
		}
	}
	return nil
}

func persistEngineIfDirty(e *Engine) error {
	if e == nil || !e.isDirty() {
		return nil
	}
	return e.saveBody(e.bodyPath)
}

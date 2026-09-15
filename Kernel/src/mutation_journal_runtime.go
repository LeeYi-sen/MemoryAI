package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

const (
	mutationJournalCommentSize = 65535
	mutationJournalSlotSize    = 32760
	mutationJournalHeaderSize  = 64
	mutationJournalMagic       = "MAJRN001"
	mutationJournalVersion     = 1
)

type mutationJournalMutation struct {
	ID      string  `json:"id"`
	Deleted bool    `json:"deleted,omitempty"`
	Memory  *Memory `json:"memory,omitempty"`
}

type mutationJournalPayload struct {
	Version   int                       `json:"version"`
	Base      string                    `json:"base"`
	Mutations []mutationJournalMutation `json:"mutations"`
}

type mutationJournalRecord struct {
	seq     uint64
	payload mutationJournalPayload
	raw     []byte
}

var errMutationJournalUnavailable = errors.New("mutation journal unavailable")

func mutationJournalMaxEntries() int {
	n := 96
	if raw := os.Getenv("MEMORYAI_MUTATION_JOURNAL_MAX_ENTRIES"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			n = parsed
		}
	}
	return n
}

func mutationJournalPayloadCapacity() int {
	return mutationJournalSlotSize - mutationJournalHeaderSize
}

func mutationJournalBaseFingerprint(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	var mf Manifest
	if err := readJSONZip(&zr.Reader, "manifest.json", &mf); err != nil {
		return "", err
	}
	canonical := struct {
		Format    string            `json:"format"`
		MemoryABI string            `json:"memory_abi"`
		BodyID    string            `json:"body_id"`
		Root      string            `json:"root"`
		Hashes    map[string]string `json:"hashes"`
		Store     StoreManifest     `json:"store"`
	}{mf.Format, mf.MemoryABI, mf.BodyID, mf.Root, mf.Hashes, mf.Store}
	b, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:]), nil
}

func mutationJournalRegion(path string) (int64, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return 0, err
	}
	commentLen := len(zr.Comment)
	zr.Close()
	if commentLen != mutationJournalCommentSize {
		return 0, errMutationJournalUnavailable
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.Size() < mutationJournalCommentSize {
		return 0, fmt.Errorf("Memory body too small for mutation journal")
	}
	return info.Size() - mutationJournalCommentSize, nil
}

func decodeMutationJournalSlot(slot []byte) (*mutationJournalRecord, bool) {
	if len(slot) != mutationJournalSlotSize || string(slot[:8]) != mutationJournalMagic {
		return nil, false
	}
	seq := binary.LittleEndian.Uint64(slot[8:16])
	n := int(binary.LittleEndian.Uint32(slot[16:20]))
	if seq == 0 || n <= 0 || n > mutationJournalPayloadCapacity() {
		return nil, false
	}
	raw := append([]byte(nil), slot[mutationJournalHeaderSize:mutationJournalHeaderSize+n]...)
	want := slot[20:52]
	got := sha256.Sum256(raw)
	if !equalBytes(want, got[:]) {
		return nil, false
	}
	var payload mutationJournalPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Version != mutationJournalVersion {
		return nil, false
	}
	return &mutationJournalRecord{seq: seq, payload: payload, raw: raw}, true
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func readMutationJournal(path string) (*mutationJournalRecord, error) {
	off, err := mutationJournalRegion(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	region := make([]byte, mutationJournalCommentSize)
	if _, err := f.ReadAt(region, off); err != nil && err != io.EOF {
		return nil, err
	}
	var best *mutationJournalRecord
	for i := 0; i < 2; i++ {
		start := i * mutationJournalSlotSize
		rec, ok := decodeMutationJournalSlot(region[start : start+mutationJournalSlotSize])
		if ok && (best == nil || rec.seq > best.seq) {
			best = rec
		}
	}
	return best, nil
}

func writeMutationJournal(path string, previousSeq uint64, raw []byte) error {
	if len(raw) == 0 || len(raw) > mutationJournalPayloadCapacity() {
		return fmt.Errorf("mutation journal payload exceeds slot capacity: %d", len(raw))
	}
	off, err := mutationJournalRegion(path)
	if err != nil {
		return err
	}
	next := previousSeq + 1
	slotIndex := int((next - 1) % 2)
	slot := make([]byte, mutationJournalSlotSize)
	copy(slot[:8], []byte(mutationJournalMagic))
	binary.LittleEndian.PutUint64(slot[8:16], next)
	binary.LittleEndian.PutUint32(slot[16:20], uint32(len(raw)))
	sum := sha256.Sum256(raw)
	copy(slot[20:52], sum[:])
	copy(slot[mutationJournalHeaderSize:], raw)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteAt(slot, off+int64(slotIndex*mutationJournalSlotSize)); err != nil {
		return err
	}
	return f.Sync()
}

func clearMutationJournal(path string) error {
	off, err := mutationJournalRegion(path)
	if err != nil {
		if errors.Is(err, errMutationJournalUnavailable) {
			return nil
		}
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	zeros := make([]byte, mutationJournalCommentSize)
	if _, err := f.WriteAt(zeros, off); err != nil {
		return err
	}
	return f.Sync()
}

func snapshotMutationJournal(e *Engine, base string) (mutationJournalPayload, []byte, error) {
	payload := mutationJournalPayload{Version: mutationJournalVersion, Base: base}
	e.dataMu.RLock()
	ids := map[string]bool{}
	for id := range e.dirtyIDs {
		ids[id] = true
	}
	for id := range e.newIDs {
		ids[id] = true
	}
	for id, yes := range e.deletedIDs {
		if yes {
			ids[id] = true
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		if e.deletedIDs[id] {
			payload.Mutations = append(payload.Mutations, mutationJournalMutation{ID: id, Deleted: true})
			continue
		}
		m := e.cache[id]
		if m == nil {
			e.dataMu.RUnlock()
			return payload, nil, fmt.Errorf("dirty Memory missing from cache: %s", id)
		}
		payload.Mutations = append(payload.Mutations, mutationJournalMutation{ID: id, Memory: copyMemory(m)})
	}
	e.dataMu.RUnlock()
	raw, err := json.Marshal(payload)
	return payload, raw, err
}

func mutationJournalMatchesBase(e *Engine, payload mutationJournalPayload) (bool, error) {
	for _, mut := range payload.Mutations {
		persisted, err := e.storeGetID(mut.ID)
		if mut.Deleted {
			if errors.Is(err, io.EOF) {
				continue
			}
			if err != nil {
				return false, err
			}
			if persisted != nil {
				return false, nil
			}
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, nil
			}
			return false, err
		}
		if structuralMemoryDigest(persisted) != structuralMemoryDigest(mut.Memory) {
			return false, nil
		}
	}
	return true, nil
}

func restoreMutationJournal(e *Engine) error {
	if e == nil {
		return nil
	}
	rec, err := readMutationJournal(e.bodyPath)
	if errors.Is(err, errMutationJournalUnavailable) || rec == nil && err == nil {
		return nil
	}
	if err != nil {
		return err
	}
	base, err := mutationJournalBaseFingerprint(e.bodyPath)
	if err != nil {
		return err
	}
	if rec.payload.Base != base {
		already, er := mutationJournalMatchesBase(e, rec.payload)
		if er != nil {
			return er
		}
		if !already {
			return fmt.Errorf("mutation journal base fingerprint mismatch")
		}
		return clearMutationJournal(e.bodyPath)
	}

	e.dataMu.Lock()
	for _, mut := range rec.payload.Mutations {
		if mut.Deleted {
			delete(e.cache, mut.ID)
			delete(e.newIDs, mut.ID)
			e.dirtyIDs[mut.ID] = true
			e.deletedIDs[mut.ID] = true
			continue
		}
		cp := copyMemory(mut.Memory)
		e.cache[mut.ID] = cp
		e.dirtyIDs[mut.ID] = true
		delete(e.deletedIDs, mut.ID)
		if _, er := e.store.GetID(mut.ID); errors.Is(er, io.EOF) {
			e.newIDs[mut.ID] = true
		}
	}
	rebuildPendingTagDeltasLocked(e, e.store)
	e.dirty = len(rec.payload.Mutations) > 0
	e.dataMu.Unlock()
	return nil
}

func loadEngineWithMutationJournal(path string) (*Engine, error) {
	e, err := loadEngineCanonical(path)
	if err != nil {
		return nil, err
	}
	if err := restoreMutationJournal(e); err != nil {
		e.closeStore()
		return nil, err
	}
	return e, nil
}

func persistEngineIncremental(e *Engine) error {
	if e == nil || !e.isDirty() {
		return nil
	}
	unlock := lockEnginePersistence(e)
	base, err := mutationJournalBaseFingerprint(e.bodyPath)
	if err != nil {
		unlock()
		return err
	}
	payload, raw, err := snapshotMutationJournal(e, base)
	if err != nil {
		unlock()
		return err
	}
	if len(payload.Mutations) == 0 {
		unlock()
		return nil
	}
	current, readErr := readMutationJournal(e.bodyPath)
	if readErr != nil && !errors.Is(readErr, errMutationJournalUnavailable) {
		unlock()
		return readErr
	}
	if len(payload.Mutations) > mutationJournalMaxEntries() || len(raw) > mutationJournalPayloadCapacity() || errors.Is(readErr, errMutationJournalUnavailable) {
		unlock()
		return e.saveBody(e.bodyPath)
	}
	if current != nil && equalBytes(current.raw, raw) {
		unlock()
		return nil
	}
	previousSeq := uint64(0)
	if current != nil {
		if current.payload.Base != base {
			unlock()
			return fmt.Errorf("active mutation journal base fingerprint mismatch")
		}
		previousSeq = current.seq
	}
	err = writeMutationJournal(e.bodyPath, previousSeq, raw)
	unlock()
	return err
}

func writeDetZipWithMutationJournal(path string, entries []zipEntry) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	if err := zw.SetComment(string(make([]byte, mutationJournalCommentSize))); err != nil {
		f.Close()
		return err
	}
	epoch := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, entry := range entries {
		method := uint16(zip.Deflate)
		if entry.store {
			method = zip.Store
		}
		h := &zip.FileHeader{Name: entry.name, Method: method}
		h.SetModTime(epoch)
		h.SetMode(0644)
		w, er := zw.CreateHeader(h)
		if er != nil {
			zw.Close()
			f.Close()
			return er
		}
		if _, er = w.Write(entry.b); er != nil {
			zw.Close()
			f.Close()
			return er
		}
	}
	if err = zw.Close(); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

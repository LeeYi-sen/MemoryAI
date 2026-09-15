package main

import (
	"archive/zip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"sort"
)

const indexEntrySize = 24
const physicalIndexPrefix = "\x1fmemoryai.phys.v1:"

type StoreManifest struct {
	Records     string `json:"records"`
	IDIndex     string `json:"id_index"`
	TagIndex    string `json:"tag_index"`
	TagLists    string `json:"tag_lists"`
	MemoryCount int    `json:"memory_count"`
}

type section struct {
	off  int64
	size int64
}
type indexEntry struct {
	hash   uint64
	off    uint64
	length uint32
	count  uint32
}

type IndexedStore struct {
	file        *os.File
	records     section
	ids         section
	tags        section
	tagLists    section
	memoryCount int
}

func hash64(s string) uint64                 { h := fnv.New64a(); _, _ = h.Write([]byte(s)); return h.Sum64() }
func physicalIndexKey(feature string) string { return physicalIndexPrefix + feature }

func openIndexedStore(body string, zr *zip.ReadCloser, sm StoreManifest) (*IndexedStore, error) {
	f, err := os.Open(body)
	if err != nil {
		return nil, err
	}
	get := func(name string) (section, error) {
		for _, zf := range zr.File {
			if zf.Name == name {
				if zf.Method != zip.Store {
					return section{}, fmt.Errorf("indexed section %s must use zip.Store", name)
				}
				o, er := zf.DataOffset()
				if er != nil {
					return section{}, er
				}
				return section{o, int64(zf.UncompressedSize64)}, nil
			}
		}
		return section{}, fmt.Errorf("indexed section %s missing", name)
	}
	rec, err := get(sm.Records)
	if err != nil {
		f.Close()
		return nil, err
	}
	ids, err := get(sm.IDIndex)
	if err != nil {
		f.Close()
		return nil, err
	}
	tags, err := get(sm.TagIndex)
	if err != nil {
		f.Close()
		return nil, err
	}
	lists, err := get(sm.TagLists)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &IndexedStore{file: f, records: rec, ids: ids, tags: tags, tagLists: lists, memoryCount: sm.MemoryCount}, nil
}
func (s *IndexedStore) Close() {
	if s != nil && s.file != nil {
		_ = s.file.Close()
	}
}
func (s *IndexedStore) readIndex(sec section, i int64) (indexEntry, error) {
	if i < 0 || i*indexEntrySize >= sec.size {
		return indexEntry{}, io.EOF
	}
	b := make([]byte, indexEntrySize)
	_, err := s.file.ReadAt(b, sec.off+i*indexEntrySize)
	if err != nil {
		return indexEntry{}, err
	}
	return indexEntry{binary.LittleEndian.Uint64(b[0:8]), binary.LittleEndian.Uint64(b[8:16]), binary.LittleEndian.Uint32(b[16:20]), binary.LittleEndian.Uint32(b[20:24])}, nil
}
func (s *IndexedStore) findHash(sec section, h uint64) (int64, error) {
	n := sec.size / indexEntrySize
	lo, hi := int64(0), n
	for lo < hi {
		mid := (lo + hi) / 2
		e, err := s.readIndex(sec, mid)
		if err != nil {
			return -1, err
		}
		if e.hash < h {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= n {
		return -1, io.EOF
	}
	e, err := s.readIndex(sec, lo)
	if err != nil {
		return -1, err
	}
	if e.hash != h {
		return -1, io.EOF
	}
	return lo, nil
}
func (s *IndexedStore) readRecord(e indexEntry) (*Memory, error) {
	b := make([]byte, e.length)
	_, err := s.file.ReadAt(b, s.records.off+int64(e.off))
	if err != nil {
		return nil, err
	}
	var m Memory
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
func (s *IndexedStore) GetID(id string) (*Memory, error) {
	h := hash64(id)
	i, err := s.findHash(s.ids, h)
	if err != nil {
		return nil, err
	}
	n := s.ids.size / indexEntrySize
	for ; i < n; i++ {
		e, er := s.readIndex(s.ids, i)
		if er != nil {
			return nil, er
		}
		if e.hash != h {
			break
		}
		m, er := s.readRecord(e)
		if er != nil {
			return nil, er
		}
		if m.ID == id {
			return m, nil
		}
	}
	return nil, io.EOF
}
func (s *IndexedStore) TagIDs(tag string) ([]string, error) {
	h := hash64(tag)
	i, err := s.findHash(s.tags, h)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	n := s.tags.size / indexEntrySize
	for ; i < n; i++ {
		e, er := s.readIndex(s.tags, i)
		if er != nil {
			return nil, er
		}
		if e.hash != h {
			break
		}
		b := make([]byte, e.length)
		_, er = s.file.ReadAt(b, s.tagLists.off+int64(e.off))
		if er != nil {
			return nil, er
		}
		var payload struct {
			Tag string   `json:"tag"`
			IDs []string `json:"ids"`
		}
		if er = json.Unmarshal(b, &payload); er != nil {
			return nil, er
		}
		if payload.Tag == tag {
			return payload.IDs, nil
		}
	}
	return nil, nil
}

// PhysicalFeatureIDs resolves an opaque activation feature through the same
// persisted hash/list machinery as tags. The reserved prefix keeps these
// physical accelerator keys separate from Memory-authored semantic tags.
func (s *IndexedStore) PhysicalFeatureIDs(feature string) ([]string, error) {
	return s.TagIDs(physicalIndexKey(feature))
}

// FirstID reads one indexed record in O(1). It is used only to probe whether a
// store already carries the persisted physical feature index; it is not a
// cognitive selection primitive.
func (s *IndexedStore) FirstID() (string, error) {
	if s == nil || s.ids.size < indexEntrySize {
		return "", io.EOF
	}
	e, err := s.readIndex(s.ids, 0)
	if err != nil {
		return "", err
	}
	m, err := s.readRecord(e)
	if err != nil {
		return "", err
	}
	return m.ID, nil
}

// HasPhysicalFeatureIndex distinguishes legacy Memory.mem stores from stores
// rebuilt by the current source. New stores can activate directly from their
// persisted exact index and avoid an O(N) startup scan.
func (s *IndexedStore) HasPhysicalFeatureIndex() (bool, error) {
	if s == nil || s.memoryCount == 0 {
		return true, nil
	}
	id, err := s.FirstID()
	if err != nil {
		return false, err
	}
	ids, err := s.PhysicalFeatureIDs("id:" + id)
	if err != nil {
		return false, err
	}
	for _, candidate := range ids {
		if candidate == id {
			return true, nil
		}
	}
	return false, nil
}

func (s *IndexedStore) AllIDs() ([]string, error) {
	n := s.ids.size / indexEntrySize
	out := make([]string, 0, n)
	for i := int64(0); i < n; i++ {
		e, err := s.readIndex(s.ids, i)
		if err != nil {
			return nil, err
		}
		m, err := s.readRecord(e)
		if err != nil {
			return nil, err
		}
		out = append(out, m.ID)
	}
	return out, nil
}

type buildRec struct {
	h      uint64
	off    uint64
	length uint32
	count  uint32
	id     string
}

func buildIndexedSections(memories []*Memory) (records, ididx, tagidx, taglists []byte, err error) {
	// Deterministic physical order by Memory ID.
	ms := append([]*Memory(nil), memories...)
	sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
	idrecs := make([]buildRec, 0, len(ms))
	// The existing tag index is also the physical secondary-index container.
	// Reserved physical keys are invisible to Memory semantics but let startup
	// activation avoid rebuilding an in-RAM index by scanning every record.
	tags := map[string][]string{}
	var off uint64
	for _, m := range ms {
		b, e := json.Marshal(m)
		if e != nil {
			return nil, nil, nil, nil, e
		}
		records = append(records, b...)
		idrecs = append(idrecs, buildRec{hash64(m.ID), off, uint32(len(b)), 0, m.ID})
		off += uint64(len(b))

		keys := map[string]struct{}{}
		for _, t := range m.Tags {
			keys[t] = struct{}{}
		}
		for _, feature := range activationFeaturesForMemory(m, 0) {
			keys[physicalIndexKey(feature)] = struct{}{}
		}
		for key := range keys {
			tags[key] = append(tags[key], m.ID)
		}
	}
	sort.Slice(idrecs, func(i, j int) bool {
		if idrecs[i].h != idrecs[j].h {
			return idrecs[i].h < idrecs[j].h
		}
		return idrecs[i].id < idrecs[j].id
	})
	ididx = encodeIndex(idrecs)
	trecs := []buildRec{}
	off = 0
	tnames := make([]string, 0, len(tags))
	for t := range tags {
		tnames = append(tnames, t)
	}
	sort.Strings(tnames)
	for _, t := range tnames {
		sort.Strings(tags[t])
		b, e := json.Marshal(struct {
			Tag string   `json:"tag"`
			IDs []string `json:"ids"`
		}{t, tags[t]})
		if e != nil {
			return nil, nil, nil, nil, e
		}
		taglists = append(taglists, b...)
		trecs = append(trecs, buildRec{hash64(t), off, uint32(len(b)), uint32(len(tags[t])), t})
		off += uint64(len(b))
	}
	sort.Slice(trecs, func(i, j int) bool {
		if trecs[i].h != trecs[j].h {
			return trecs[i].h < trecs[j].h
		}
		return trecs[i].id < trecs[j].id
	})
	tagidx = encodeIndex(trecs)
	return
}
func encodeIndex(xs []buildRec) []byte {
	b := make([]byte, len(xs)*indexEntrySize)
	for i, e := range xs {
		o := i * indexEntrySize
		binary.LittleEndian.PutUint64(b[o:o+8], e.h)
		binary.LittleEndian.PutUint64(b[o+8:o+16], e.off)
		binary.LittleEndian.PutUint32(b[o+16:o+20], e.length)
		binary.LittleEndian.PutUint32(b[o+20:o+24], e.count)
	}
	return b
}

func validateIndexedStore(s *IndexedStore) error {
	if s == nil {
		return errors.New("store nil")
	}
	if s.ids.size%indexEntrySize != 0 || s.tags.size%indexEntrySize != 0 {
		return errors.New("index size invalid")
	}
	return nil
}

package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	legacyIndexEntrySize = 24
	indexEntrySize       = 56
	indexFormatV2        = "memoryai-index-v2-sha256"
)
const physicalIndexPrefix = "\x1fmemoryai.phys.v1:"

type StoreManifest struct {
	Records     string `json:"records"`
	IDIndex     string `json:"id_index"`
	TagIndex    string `json:"tag_index"`
	TagLists    string `json:"tag_lists"`
	IndexFormat string `json:"index_format,omitempty"`
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
	digest [32]byte
}

type IndexedStore struct {
	file        *os.File
	records     section
	ids         section
	tags        section
	tagLists    section
	memoryCount int
	entrySize   int64
	hasDigests  bool
}

func hash64(s string) uint64                 { h := fnv.New64a(); _, _ = h.Write([]byte(s)); return h.Sum64() }
func physicalIndexKey(feature string) string { return physicalIndexPrefix + feature }

func openIndexedStore(body string, zr *zip.ReadCloser, sm StoreManifest) (*IndexedStore, error) {
	f, err := os.Open(body)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	fileSize := info.Size()
	get := func(name string) (section, error) {
		zf, err := uniqueZipFile(&zr.Reader, name)
		if err != nil {
			return section{}, err
		}
		if zf.Method != zip.Store {
			return section{}, fmt.Errorf("indexed section %s must use zip.Store", name)
		}
		if zf.UncompressedSize64 > uint64(1<<63-1) {
			return section{}, fmt.Errorf("indexed section %s size overflows physical offset", name)
		}
		o, er := zf.DataOffset()
		if er != nil {
			return section{}, er
		}
		size := int64(zf.UncompressedSize64)
		if o < 0 || size < 0 || o > fileSize || size > fileSize-o {
			return section{}, fmt.Errorf("indexed section %s escapes physical body file", name)
		}
		return section{o, size}, nil
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
	entrySize := int64(legacyIndexEntrySize)
	hasDigests := false
	switch strings.TrimSpace(sm.IndexFormat) {
	case "":
		// Legacy v1 index remains readable. loadEngine verifies the complete
		// records/tag-list sections before exposing a legacy store.
	case indexFormatV2:
		entrySize = indexEntrySize
		hasDigests = true
	default:
		f.Close()
		return nil, fmt.Errorf("unsupported Memory index format %q", sm.IndexFormat)
	}
	if ids.size%entrySize != 0 || tags.size%entrySize != 0 {
		f.Close()
		return nil, errors.New("index section size invalid")
	}
	if sm.MemoryCount < 0 || int64(sm.MemoryCount) != ids.size/entrySize {
		f.Close()
		return nil, fmt.Errorf("manifest memory_count does not match physical ID index")
	}
	return &IndexedStore{
		file: f, records: rec, ids: ids, tags: tags, tagLists: lists,
		memoryCount: sm.MemoryCount, entrySize: entrySize, hasDigests: hasDigests,
	}, nil
}
func (s *IndexedStore) Close() {
	if s != nil && s.file != nil {
		_ = s.file.Close()
	}
}
func (s *IndexedStore) readIndex(sec section, i int64) (indexEntry, error) {
	if s == nil || s.entrySize < legacyIndexEntrySize {
		return indexEntry{}, errors.New("indexed store entry size invalid")
	}
	if i < 0 || i*s.entrySize >= sec.size {
		return indexEntry{}, io.EOF
	}
	b := make([]byte, int(s.entrySize))
	_, err := s.file.ReadAt(b, sec.off+i*s.entrySize)
	if err != nil {
		return indexEntry{}, err
	}
	e := indexEntry{
		hash:   binary.LittleEndian.Uint64(b[0:8]),
		off:    binary.LittleEndian.Uint64(b[8:16]),
		length: binary.LittleEndian.Uint32(b[16:20]),
		count:  binary.LittleEndian.Uint32(b[20:24]),
	}
	if s.hasDigests {
		if len(b) != indexEntrySize {
			return indexEntry{}, fmt.Errorf("digest index entry size invalid: %d", len(b))
		}
		copy(e.digest[:], b[24:56])
	}
	return e, nil
}
func (s *IndexedStore) findHash(sec section, h uint64) (int64, error) {
	if s == nil || s.entrySize <= 0 {
		return -1, errors.New("indexed store entry size invalid")
	}
	n := sec.size / s.entrySize
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
	if err := indexedSliceBounds(s.records, e.off, e.length, hardMemoryRecordMaxBytes, "Memory record"); err != nil {
		return nil, err
	}
	b := make([]byte, e.length)
	_, err := s.file.ReadAt(b, s.records.off+int64(e.off))
	if err != nil {
		return nil, err
	}
	if s.hasDigests {
		got := sha256.Sum256(b)
		if got != e.digest {
			return nil, errors.New("Memory record digest mismatch")
		}
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
	n := s.ids.size / s.entrySize
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
	n := s.tags.size / s.entrySize
	for ; i < n; i++ {
		e, er := s.readIndex(s.tags, i)
		if er != nil {
			return nil, er
		}
		if e.hash != h {
			break
		}
		if er = indexedSliceBounds(s.tagLists, e.off, e.length, hardTagListPayloadMaxBytes, "tag-list payload"); er != nil {
			return nil, er
		}
		b := make([]byte, e.length)
		_, er = s.file.ReadAt(b, s.tagLists.off+int64(e.off))
		if er != nil {
			return nil, er
		}
		if s.hasDigests {
			got := sha256.Sum256(b)
			if got != e.digest {
				return nil, errors.New("tag-list payload digest mismatch")
			}
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
	if s == nil || s.entrySize <= 0 || s.ids.size < s.entrySize {
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
	n := s.ids.size / s.entrySize
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
	digest [32]byte
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
		if uint64(len(b)) > hardMemoryRecordMaxBytes {
			return nil, nil, nil, nil, fmt.Errorf("Memory record exceeds physical byte ceiling: id=%s size=%d max=%d", m.ID, len(b), hardMemoryRecordMaxBytes)
		}
		records = append(records, b...)
		digest := sha256.Sum256(b)
		idrecs = append(idrecs, buildRec{hash64(m.ID), off, uint32(len(b)), 0, digest, m.ID})
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
		if uint64(len(b)) > hardTagListPayloadMaxBytes {
			return nil, nil, nil, nil, fmt.Errorf("tag-list payload exceeds physical byte ceiling: tag=%s size=%d max=%d", t, len(b), hardTagListPayloadMaxBytes)
		}
		taglists = append(taglists, b...)
		digest := sha256.Sum256(b)
		trecs = append(trecs, buildRec{hash64(t), off, uint32(len(b)), uint32(len(tags[t])), digest, t})
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
		copy(b[o+24:o+56], e.digest[:])
	}
	return b
}

func validateIndexedStore(s *IndexedStore) error {
	if s == nil {
		return errors.New("store nil")
	}
	if s.entrySize != legacyIndexEntrySize && s.entrySize != indexEntrySize {
		return errors.New("index entry size invalid")
	}
	if s.ids.size%s.entrySize != 0 || s.tags.size%s.entrySize != 0 {
		return errors.New("index size invalid")
	}
	if int64(s.memoryCount) != s.ids.size/s.entrySize {
		return errors.New("memory count/index cardinality mismatch")
	}
	return nil
}

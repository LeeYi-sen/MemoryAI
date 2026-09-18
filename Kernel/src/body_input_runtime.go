package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const (
	hardJSONZipEntryMaxBytes   int64  = 4 << 20
	hardOpaqueZipEntryMaxBytes int64  = 16 << 20
	hardMemoryRecordMaxBytes   uint64 = 16 << 20
	hardTagListPayloadMaxBytes uint64 = 64 << 20
)

func validateUniqueZipEntryNames(zr *zip.Reader) error {
	if zr == nil {
		return fmt.Errorf("Memory ZIP reader unavailable")
	}
	seen := map[string]struct{}{}
	for _, file := range zr.File {
		name := file.Name
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate Memory ZIP entry %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func uniqueZipFile(zr *zip.Reader, name string) (*zip.File, error) {
	if zr == nil {
		return nil, fmt.Errorf("Memory ZIP reader unavailable")
	}
	var found *zip.File
	for _, file := range zr.File {
		if file.Name != name {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("duplicate Memory ZIP entry %q", name)
		}
		found = file
	}
	if found == nil {
		return nil, fmt.Errorf("%s missing", name)
	}
	return found, nil
}

func readZipFileBounded(file *zip.File, maxBytes int64, label string) ([]byte, error) {
	if file == nil {
		return nil, fmt.Errorf("%s zip entry unavailable", label)
	}
	if file.UncompressedSize64 > uint64(maxBytes) {
		return nil, fmt.Errorf("%s zip entry exceeds physical byte ceiling: size=%d max=%d", label, file.UncompressedSize64, maxBytes)
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return readAllPhysicalBounded(r, maxBytes, label+" zip entry")
}

func hashZipFileStreaming(file *zip.File) (string, error) {
	if file == nil {
		return "", fmt.Errorf("zip entry unavailable")
	}
	r, err := file.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func indexedSliceBounds(sec section, off uint64, length uint32, hardMax uint64, label string) error {
	if uint64(length) > hardMax {
		return fmt.Errorf("%s exceeds physical byte ceiling: length=%d max=%d", label, length, hardMax)
	}
	if sec.off < 0 || sec.size < 0 {
		return fmt.Errorf("%s section bounds invalid", label)
	}
	sectionSize := uint64(sec.size)
	if off > sectionSize || uint64(length) > sectionSize-off {
		return fmt.Errorf("%s index slice escapes section: off=%d length=%d section=%d", label, off, length, sectionSize)
	}
	return nil
}

func verifyZipEntryHash(zr *zip.Reader, name, expected string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if len(expected) != sha256.Size*2 {
		return fmt.Errorf("Memory manifest hash for %s is invalid", name)
	}
	file, err := uniqueZipFile(zr, name)
	if err != nil {
		return err
	}
	got, err := hashZipFileStreaming(file)
	if err != nil {
		return err
	}
	if got != expected {
		return fmt.Errorf("hash mismatch %s", name)
	}
	return nil
}

func verifyBodyLoadIntegrity(zr *zip.Reader, mf Manifest) error {
	if zr == nil {
		return fmt.Errorf("Memory ZIP reader unavailable")
	}
	required := []string{
		strings.TrimSpace(mf.GenesisPath),
		strings.TrimSpace(mf.Store.Records),
		strings.TrimSpace(mf.Store.IDIndex),
		strings.TrimSpace(mf.Store.TagIndex),
		strings.TrimSpace(mf.Store.TagLists),
	}
	seen := map[string]struct{}{}
	for _, name := range required {
		if name == "" {
			return fmt.Errorf("Memory manifest store entry name missing")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("Memory manifest aliases physical entry %q", name)
		}
		seen[name] = struct{}{}
		if strings.TrimSpace(mf.Hashes[name]) == "" {
			return fmt.Errorf("Memory manifest hash missing for %s", name)
		}
	}

	verify := []string{mf.GenesisPath, mf.Store.IDIndex, mf.Store.TagIndex}
	switch strings.TrimSpace(mf.Store.IndexFormat) {
	case "":
		// Legacy v1 entries do not carry per-record digests, so the complete
		// record and posting sections must be verified before the store is used.
		verify = append(verify, mf.Store.Records, mf.Store.TagLists)
	case indexFormatV2:
		// V2 authenticates the indexes at load time. Those verified indexes carry
		// SHA-256 for each record/posting, which is checked lazily on access.
	default:
		return fmt.Errorf("unsupported Memory index format %q", mf.Store.IndexFormat)
	}
	for _, name := range verify {
		if err := verifyZipEntryHash(zr, name, mf.Hashes[name]); err != nil {
			return err
		}
	}
	return nil
}

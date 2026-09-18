package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPhysicalExchangeMaxBytes int64 = 1 << 20
	hardPhysicalExchangeMaxBytes    int64 = 16 << 20
	defaultArtifactMaxBytes         int64 = 4 << 20
	hardArtifactMaxBytes            int64 = 64 << 20
	defaultDaemonTransportMaxBytes  int64 = 1 << 20
	hardDaemonTransportMaxBytes     int64 = 8 << 20
	defaultMeshTransportMaxBytes    int64 = 2 << 20
	hardMeshTransportMaxBytes       int64 = 16 << 20
	defaultStorageTransportMaxBytes int64 = 20 << 20
	hardStorageTransportMaxBytes    int64 = 32 << 20
	defaultPhysicalExchangeTimeout        = 5 * time.Second
	hardPhysicalExchangeTimeout           = 60 * time.Second
	defaultFrameListMaxItems              = 8192
	hardFrameListMaxItems                 = 65536
	defaultFrameValueMaxBytes       int64 = 16 << 20
	hardFrameValueMaxBytes          int64 = 64 << 20
	defaultFrameOutputMaxItems            = 4096
	hardFrameOutputMaxItems               = 32768
	defaultFrameOutputMaxBytes      int64 = 16 << 20
	hardFrameOutputMaxBytes         int64 = 64 << 20
)

func boundedPhysicalByteEnv(name string, fallback, hardMax int64) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return fallback
	}
	if value > hardMax {
		return hardMax
	}
	return value
}

func boundedPhysicalCountEnv(name string, fallback, hardMax int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	if value > hardMax {
		return hardMax
	}
	return value
}

func frameListMaxItems() int {
	return boundedPhysicalCountEnv(
		"MEMORYAI_FRAME_LIST_MAX_ITEMS",
		defaultFrameListMaxItems,
		hardFrameListMaxItems,
	)
}

func ensureFrameListItems(count int, label string) error {
	maxItems := frameListMaxItems()
	if count > maxItems {
		return fmt.Errorf("%s exceeds physical Frame-list cardinality: items=%d max=%d", label, count, maxItems)
	}
	return nil
}

func frameValueMaxBytes() int64 {
	return boundedPhysicalByteEnv(
		"MEMORYAI_FRAME_VALUE_MAX_BYTES",
		defaultFrameValueMaxBytes,
		hardFrameValueMaxBytes,
	)
}

func frameOutputMaxItems() int {
	return boundedPhysicalCountEnv(
		"MEMORYAI_FRAME_OUTPUT_MAX_ITEMS",
		defaultFrameOutputMaxItems,
		hardFrameOutputMaxItems,
	)
}

func frameOutputMaxBytes() int64 {
	return boundedPhysicalByteEnv(
		"MEMORYAI_FRAME_OUTPUT_MAX_BYTES",
		defaultFrameOutputMaxBytes,
		hardFrameOutputMaxBytes,
	)
}

func ensureFrameValueBytes(count int, label string) error {
	maxBytes := frameValueMaxBytes()
	if int64(count) > maxBytes {
		return fmt.Errorf("%s exceeds physical Frame-value byte ceiling: bytes=%d max=%d", label, count, maxBytes)
	}
	return nil
}

func ensureFrameJoinBytes(label string, parts ...string) error {
	maxBytes := frameValueMaxBytes()
	total := int64(0)
	for _, part := range parts {
		if int64(len(part)) > maxBytes-total {
			return fmt.Errorf("%s exceeds physical Frame-value byte ceiling: max=%d", label, maxBytes)
		}
		total += int64(len(part))
	}
	return nil
}

func ensureFrameOutputAppend(output []string, value string) error {
	if err := ensureFrameValueBytes(len(value), "emit"); err != nil {
		return err
	}
	maxItems := frameOutputMaxItems()
	if len(output)+1 > maxItems {
		return fmt.Errorf("emit exceeds physical Frame-output cardinality: items=%d max=%d", len(output)+1, maxItems)
	}
	maxBytes := frameOutputMaxBytes()
	total := int64(len(value))
	for _, existing := range output {
		if int64(len(existing)) > maxBytes-total {
			return fmt.Errorf("emit exceeds physical Frame-output byte ceiling: max=%d", maxBytes)
		}
		total += int64(len(existing))
	}
	return nil
}

func physicalExchangeMaxBytes() int64 {
	return boundedPhysicalByteEnv(
		"MEMORYAI_PHYSICAL_EXCHANGE_MAX_BYTES",
		defaultPhysicalExchangeMaxBytes,
		hardPhysicalExchangeMaxBytes,
	)
}

func clampPhysicalTimeout(requested, fallback, hardMax time.Duration) time.Duration {
	if fallback <= 0 || hardMax <= 0 || fallback > hardMax {
		return 0
	}
	if requested <= 0 {
		return fallback
	}
	if requested > hardMax {
		return hardMax
	}
	return requested
}

func physicalExchangeTimeout(requested time.Duration) time.Duration {
	return clampPhysicalTimeout(requested, defaultPhysicalExchangeTimeout, hardPhysicalExchangeTimeout)
}

func parsePhysicalExchangeTimeoutMS(raw string) time.Duration {
	ms, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || ms <= 0 {
		return defaultPhysicalExchangeTimeout
	}
	maxMS := int64(hardPhysicalExchangeTimeout / time.Millisecond)
	if ms > maxMS {
		return hardPhysicalExchangeTimeout
	}
	return physicalExchangeTimeout(time.Duration(ms) * time.Millisecond)
}

func artifactMaxBytes() int64 {
	return boundedPhysicalByteEnv(
		"MEMORYAI_ARTIFACT_MAX_BYTES",
		defaultArtifactMaxBytes,
		hardArtifactMaxBytes,
	)
}

func readAllPhysicalBounded(r io.Reader, maxBytes int64, label string) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("%s reader unavailable", label)
	}
	if maxBytes < 1 {
		return nil, fmt.Errorf("%s physical byte ceiling invalid: %d", label, maxBytes)
	}
	payload, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maxBytes {
		return nil, fmt.Errorf("%s response exceeds physical byte ceiling: max=%d", label, maxBytes)
	}
	return payload, nil
}

func ensureArtifactSizeWithinLimit(path string, maxBytes int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > maxBytes {
		return fmt.Errorf("artifact exceeds physical byte ceiling: size=%d max=%d", info.Size(), maxBytes)
	}
	return nil
}

func daemonTransportMaxBytes() int64 {
	return boundedPhysicalByteEnv(
		"MEMORYAI_DAEMON_MAX_BYTES",
		defaultDaemonTransportMaxBytes,
		hardDaemonTransportMaxBytes,
	)
}

func meshTransportMaxBytes() int64 {
	return boundedPhysicalByteEnv(
		"MEMORYAI_MESH_MAX_BYTES",
		defaultMeshTransportMaxBytes,
		hardMeshTransportMaxBytes,
	)
}

func storageTransportMaxBytes() int64 {
	return boundedPhysicalByteEnv(
		"MEMORYAI_STORAGE_MAX_BYTES",
		defaultStorageTransportMaxBytes,
		hardStorageTransportMaxBytes,
	)
}

type physicalByteCeilingBuffer struct {
	buf bytes.Buffer
	max int64
}

func (w *physicalByteCeilingBuffer) Write(p []byte) (int, error) {
	if w == nil || w.max < 1 {
		return 0, fmt.Errorf("physical byte ceiling writer unavailable")
	}
	if int64(w.buf.Len())+int64(len(p)) > w.max {
		return 0, fmt.Errorf("encoded payload exceeds physical byte ceiling: max=%d", w.max)
	}
	return w.buf.Write(p)
}

func encodeJSONPhysicalBounded(v any, maxBytes int64, label string) ([]byte, error) {
	if maxBytes < 1 {
		return nil, fmt.Errorf("%s physical byte ceiling invalid: %d", label, maxBytes)
	}
	w := &physicalByteCeilingBuffer{max: maxBytes}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return append([]byte(nil), w.buf.Bytes()...), nil
}

package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const (
	defaultPhysicalExchangeMaxBytes int64 = 1 << 20
	hardPhysicalExchangeMaxBytes    int64 = 16 << 20
	defaultArtifactMaxBytes         int64 = 4 << 20
	hardArtifactMaxBytes            int64 = 64 << 20
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

func physicalExchangeMaxBytes() int64 {
	return boundedPhysicalByteEnv(
		"MEMORYAI_PHYSICAL_EXCHANGE_MAX_BYTES",
		defaultPhysicalExchangeMaxBytes,
		hardPhysicalExchangeMaxBytes,
	)
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

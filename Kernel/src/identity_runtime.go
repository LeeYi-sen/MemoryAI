package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

var identitySequence uint64
var identityProcessNonce = newIdentityProcessNonce()

func newIdentityProcessNonce() string {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err == nil {
		return hex.EncodeToString(entropy[:])
	}
	fallback := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", os.Getpid(), time.Now().UnixNano())))
	return hex.EncodeToString(fallback[:16])
}

// nextID is a physical identity allocator. Uniqueness never depends on wall-clock
// granularity: one process-wide nonce separates process lifetimes and an atomic
// sequence separates every allocation inside the current process.
func nextID(prefix string) string {
	sequence := atomic.AddUint64(&identitySequence, 1)
	return fmt.Sprintf("%s-%s-%016x", prefix, identityProcessNonce, sequence)
}

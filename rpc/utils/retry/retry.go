package retry

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"time"
)

func rand_Float64() float64 {
	// read a random 64-bit float from crypto/rand.Reader
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b))
}

// CalculateBackoff determines the backoff duration for retries.
// The algorithm matches CLAM Core's exponential backoff with jitter strategy.
func CalculateBackoff(attempt int, min, max time.Duration) time.Duration {
	// Use exponential backoff: min * 2^attempt
	backoff := min * time.Duration(1<<uint(attempt))
	if backoff > max {
		backoff = max
	}

	// Add jitter (±20%) to prevent thundering herd
	// Matches CLAM Core's retry jitter implementation
	jitter := time.Duration(rand_Float64()*0.4*float64(backoff)) - (backoff / 5)
	return backoff + jitter
}

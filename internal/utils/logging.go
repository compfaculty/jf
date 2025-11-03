package utils

import (
	"log"
	"sync/atomic"
)

var (
	verboseFlag int32 // 0 = false, 1 = true (atomic for thread safety)
)

// SetVerbose enables or disables verbose logging mode.
// This is thread-safe and can be called from multiple goroutines.
func SetVerbose(enabled bool) {
	if enabled {
		atomic.StoreInt32(&verboseFlag, 1)
	} else {
		atomic.StoreInt32(&verboseFlag, 0)
	}
}

// IsVerbose returns true if verbose logging is enabled.
// This is thread-safe and can be called from multiple goroutines.
func IsVerbose() bool {
	return atomic.LoadInt32(&verboseFlag) == 1
}

// Verbosef logs a message only when verbose mode is enabled.
// It uses the same format as log.Printf and prefixes messages with [VERBOSE].
func Verbosef(format string, args ...any) {
	if IsVerbose() {
		log.Printf("[VERBOSE] "+format, args...)
	}
}

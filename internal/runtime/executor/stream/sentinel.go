package stream

import (
	"bytes"
	"errors"
	"hash/fnv"
)

const (
	// Default values for StreamSentinel
	DefaultMaxRepeats       = 6
	DefaultMaxHashRepeats   = 4
	DefaultHashWindowSize   = 8
	DefaultWarningThreshold = 3
	DefaultMaxBytes         = 16 * 1024 // 16KB
)

var (
	// SentinelTriggeredErr is returned when the sentinel detects a loop
	SentinelTriggeredErr = errors.New("stream sentinel: infinite loop detected")
	// SentinelWarningErr is returned as a warning before triggering
	SentinelWarningErr = errors.New("stream sentinel: warning")
)

// SentinelConfig configures stream loop detection
type SentinelConfig struct {
	Enabled          bool
	MaxRepeats       int // Default 6 - exact matches before close
	MaxHashRepeats   int // Default 4 - hash repeats in window
	HashWindowSize   int // Default 8 - window for hash detection
	WarningThreshold int // Default 3 - warn before close
	MaxBytes         int // Default 16KB - tail buffer size
}

// Option configures a StreamSentinel
type Option func(*StreamSentinel)

// WithMaxRepeats sets the max exact repeat count
func WithMaxRepeats(n int) Option {
	return func(s *StreamSentinel) {
		s.MaxRepeats = n
	}
}

// WithMaxBytes sets the tail buffer size
func WithMaxBytes(n int) Option {
	return func(s *StreamSentinel) {
		s.MaxBytes = n
	}
}

// WithWarningThreshold sets the warning threshold
func WithWarningThreshold(n int) Option {
	return func(s *StreamSentinel) {
		s.WarningThreshold = n
	}
}

// WithOnWarning sets the warning callback
func WithOnWarning(fn func(string)) Option {
	return func(s *StreamSentinel) {
		s.OnWarning = fn
	}
}

// WithOnTrigger sets the trigger callback
func WithOnTrigger(fn func(string)) Option {
	return func(s *StreamSentinel) {
		s.OnTrigger = fn
	}
}

// StreamSentinel detects infinite loops in streaming responses
// by tracking repeated chunks using both exact matching and rolling hashes.
type StreamSentinel struct {
	// Config
	MaxRepeats       int // Max exact repeats before close (default: 6)
	MaxHashRepeats   int // Max hash repeats in window (default: 4)
	HashWindowSize   int // Window for hash detection (default: 8)
	WarningThreshold int // Warn before close (default: 3)
	MaxBytes         int // Tail buffer size (default: 16KB)

	// State
	ring      [][]byte // Ring buffer of recent chunks
	hashes    []uint64 // FNV-1a hashes for boundary-shifted detection
	idx       int      // Current position in ring
	count     int      // Total chunks processed
	repeatCnt int      // Consecutive exact repeat count
	hashCnt   int      // Consecutive hash repeat count
	warned    bool     // Warning sent flag

	// Callbacks
	OnWarning func(reason string)
	OnTrigger func(reason string)
}

// NewStreamSentinel creates a new sentinel with the given options
func NewStreamSentinel(opts ...Option) *StreamSentinel {
	s := &StreamSentinel{
		MaxRepeats:       DefaultMaxRepeats,
		MaxHashRepeats:   DefaultMaxHashRepeats,
		HashWindowSize:   DefaultHashWindowSize,
		WarningThreshold: DefaultWarningThreshold,
		MaxBytes:         DefaultMaxBytes,
		hashes:           make([]uint64, DefaultHashWindowSize),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// DefaultSentinel returns a sentinel with recommended defaults
func DefaultSentinel() *StreamSentinel {
	return NewStreamSentinel()
}

// Check processes a new chunk and returns:
// - nil, nil: continue streaming
// - warning, nil: continue but emit warning
// - nil, error: terminate stream
func (s *StreamSentinel) Check(chunk []byte) (warning error, err error) {
	if len(chunk) == 0 {
		return nil, nil
	}

	// Calculate FNV-1a hash of the chunk
	h := fnv.New64a()
	h.Write(chunk)
	currentHash := h.Sum64()

	// Get the previous chunk for exact comparison
	var prevChunk []byte
	if s.count > 0 {
		prevIdx := (s.idx - 1 + len(s.ring)) % len(s.ring)
		prevChunk = s.ring[prevIdx]
	}

	// Check for exact match (same chunk repeated)
	isExactRepeat := prevChunk != nil && bytes.Equal(chunk, prevChunk)

	if isExactRepeat {
		s.repeatCnt++
	} else {
		s.repeatCnt = 0
	}

	// Check for hash repeat (boundary-shifted repetition)
	s.hashCnt = 0
	for _, hval := range s.hashes {
		if hval == currentHash && hval != 0 {
			s.hashCnt++
		}
	}

	// Store hash in rolling window
	s.hashes[s.count%len(s.hashes)] = currentHash

	// Store chunk in ring buffer if we have space
	if s.count < cap(s.ring) {
		// Copy chunk to avoid referencing scanner buffer
		chunkCopy := make([]byte, len(chunk))
		copy(chunkCopy, chunk)
		s.ring = append(s.ring, chunkCopy)
	} else {
		// Copy into ring buffer at current position
		chunkCopy := make([]byte, len(chunk))
		copy(chunkCopy, chunk)
		s.ring[s.idx] = chunkCopy
		s.idx = (s.idx + 1) % len(s.ring)
	}

	s.count++

	// Build reason string
	var reason stringsBuilder

	// Check thresholds and trigger/warn
	// Exact repeat detection
	if s.repeatCnt >= s.MaxRepeats {
		if s.OnTrigger != nil {
			s.OnTrigger("exact repeat detected")
		}
		return nil, SentinelTriggeredErr
	}

	// Hash repeat detection
	if s.hashCnt >= s.MaxHashRepeats {
		if s.OnTrigger != nil {
			s.OnTrigger("hash repeat detected")
		}
		return nil, SentinelTriggeredErr
	}

	// Warning threshold
	if !s.warned && s.repeatCnt >= s.WarningThreshold {
		s.warned = true
		reason.WriteString("close to repeat threshold")
		if s.OnWarning != nil {
			s.OnWarning(reason.String())
		}
		return SentinelWarningErr, nil
	}

	return nil, nil
}

// Reset clears the sentinel state for reuse
func (s *StreamSentinel) Reset() {
	s.idx = 0
	s.count = 0
	s.repeatCnt = 0
	s.hashCnt = 0
	s.warned = false
	clearSlice(s.ring)
	clearSlice(s.hashes)
}

// stringsBuilder is a simple wrapper for building reason strings
type stringsBuilder struct {
	s string
}

func (b *stringsBuilder) WriteString(s string) {
	if b.s != "" {
		b.s += ", "
	}
	b.s += s
}

func (b *stringsBuilder) String() string {
	return b.s
}

// clearSlice clears a slice of any type
func clearSlice[T any](s []T) {
	for i := range s {
		s[i] = *new(T)
	}
}

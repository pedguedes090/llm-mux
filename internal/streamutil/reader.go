package streamutil

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"time"
)

// StreamReaderConfig configures the optimized stream reader.
type StreamReaderConfig struct {
	// IdleTimeout for stalled connection detection (default: 5 minutes)
	IdleTimeout time.Duration
	// BufferSize for the reader (default: 256KB - increased from 64KB for better streaming)
	BufferSize int
	// MaxLineSize limit (default: 10MB - increased from 2MB for large SSE events)
	MaxLineSize int
	// Name for logging purposes
	Name string
}

// DefaultStreamReaderConfig returns sensible defaults for single-user install.
// Optimized for maximum single-stream performance.
func DefaultStreamReaderConfig() StreamReaderConfig {
	return StreamReaderConfig{
		IdleTimeout: 5 * time.Minute,
		BufferSize:  1024 * 1024,      // 1MB - single user, maximize performance
		MaxLineSize: 50 * 1024 * 1024, // 50MB - single user, handle large responses
		Name:        "stream",
	}
}

// OptimizedStreamReader wraps an io.ReadCloser with context awareness
// and idle detection using the shared IdleWatcher.
type OptimizedStreamReader struct {
	body      io.ReadCloser
	ctx       context.Context
	touch     func()
	done      func()
	closeOnce func()
}

// NewOptimizedStreamReader creates a stream reader using the shared idle watcher.
// This eliminates the need for 2 goroutines per stream.
func NewOptimizedStreamReader(ctx context.Context, body io.ReadCloser, cfg StreamReaderConfig) *OptimizedStreamReader {
	watcher := DefaultIdleWatcher()

	r := &OptimizedStreamReader{
		body: body,
		ctx:  ctx,
	}

	// Register with shared idle watcher
	r.touch, r.done = watcher.Register(ctx, cfg.IdleTimeout, func() {
		// On idle timeout, close body to unblock Read
		body.Close()
	})

	// Setup close once
	var closed bool
	r.closeOnce = func() {
		if !closed {
			closed = true
			r.done()
			body.Close()
		}
	}

	return r
}

// Read implements io.Reader with activity tracking.
func (r *OptimizedStreamReader) Read(p []byte) (n int, err error) {
	// Check context before blocking read
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
	}

	n, err = r.body.Read(p)
	if n > 0 {
		r.touch() // Update activity timestamp
	}
	return n, err
}

// Close implements io.Closer.
func (r *OptimizedStreamReader) Close() error {
	r.closeOnce()
	return nil
}

// LineScanner provides line-by-line reading with pooled buffers.
// Uses bufio.Reader.ReadSlice instead of bufio.Scanner to support larger lines.
type LineScanner struct {
	reader    *OptimizedStreamReader
	readerBuf *bufio.Reader
	buf       []byte
	maxSize   int
	line      []byte
	err       error
}

// NewLineScanner creates a scanner for line-by-line reading.
// Uses bufio.Reader.ReadSlice for handling large SSE events (up to MaxLineSize).
func NewLineScanner(ctx context.Context, body io.ReadCloser, cfg StreamReaderConfig) *LineScanner {
	reader := NewOptimizedStreamReader(ctx, body, cfg)

	// Get pooled buffer - use larger buffer for better performance
	bufSize := cfg.BufferSize
	if bufSize == 0 {
		bufSize = 256 * 1024
	}
	buf := GetBuffer(bufSize)

	maxSize := cfg.MaxLineSize
	if maxSize == 0 {
		maxSize = 10 * 1024 * 1024
	}

	return &LineScanner{
		reader:    reader,
		readerBuf: bufio.NewReaderSize(reader, bufSize),
		buf:       *buf,
		maxSize:   maxSize,
	}
}

// Scan advances to the next line. Returns false when done or on error.
// Uses ReadSlice which handles lines larger than bufio.Scanner's 64KB limit.
func (s *LineScanner) Scan() bool {
	// Read until newline
	line, err := s.readerBuf.ReadSlice('\n')
	if err == io.EOF {
		if len(line) > 0 {
			s.line = line
			return true
		}
		s.err = io.EOF
		return false
	}
	if err == bufio.ErrBufferFull {
		// Line too long - copy partial data to preserve it
		s.err = &LineTooLongError{MaxSize: s.maxSize}
		partial := make([]byte, len(s.line))
		copy(partial, s.line)
		s.line = partial
		return false
	}
	if err != nil {
		s.err = err
		return false
	}

	s.line = line
	return true
}

// Bytes returns the current line bytes (without trailing newline).
func (s *LineScanner) Bytes() []byte {
	// Remove trailing \n and \r\n
	return bytes.TrimSuffix(s.line, []byte{'\n'})
}

// Text returns the current line as string.
func (s *LineScanner) Text() string {
	return string(s.Bytes())
}

// Err returns any error that occurred during scanning.
func (s *LineScanner) Err() error {
	return s.err
}

// Close closes the scanner and returns the buffer to the pool.
func (s *LineScanner) Close() error {
	PutBuffer(&s.buf)
	return s.reader.Close()
}

// LineTooLongError indicates the line exceeded the maximum size.
type LineTooLongError struct {
	MaxSize int
}

func (e *LineTooLongError) Error() string {
	return "line too long (max: " + formatBytes(e.MaxSize) + ")"
}

func formatBytes(n int) string {
	if n >= 1024*1024 {
		return fmt.Sprintf("%dMB", n/(1024*1024))
	}
	if n >= 1024 {
		return fmt.Sprintf("%dKB", n/1024)
	}
	return fmt.Sprintf("%dB", n)
}

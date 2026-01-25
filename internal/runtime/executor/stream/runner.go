package stream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/nghyane/llm-mux/internal/config"
	log "github.com/nghyane/llm-mux/internal/logging"
	"github.com/nghyane/llm-mux/internal/provider"
	"github.com/nghyane/llm-mux/internal/sseutil"
	"github.com/nghyane/llm-mux/internal/streamutil"
	"github.com/nghyane/llm-mux/internal/translator/ir"
	"github.com/nghyane/llm-mux/internal/translator/to_ir"
	"github.com/tidwall/gjson"
)

// UsageReporter interface for usage tracking (implemented by executor.usageReporter)
type UsageReporter interface {
	Publish(ctx context.Context, u *ir.Usage)
	PublishFailure(ctx context.Context)
	EnsurePublished(ctx context.Context)
}

const (
	DefaultStreamBufferSize  = 2 * 1024 * 1024 // 2MB
	DefaultScannerBufferSize = 1024 * 1024     // 1MB - single user, maximize for single stream
	DefaultStreamIdleTimeout = 5 * time.Minute
)

var ScannerBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, DefaultScannerBufferSize)
		return &buf
	},
}

var (
	doneMarker = []byte("[DONE]")
	dataTag    = []byte("data:")
)

type StreamProcessor interface {
	ProcessLine(line []byte) (chunks [][]byte, usage *ir.Usage, err error)
	ProcessDone() (chunks [][]byte, err error)
}

type StreamPreprocessor func(line []byte) (payload []byte, skip bool)

type StreamConfig struct {
	ExecutorName       string
	MaxBufferSize      int
	Preprocessor       StreamPreprocessor
	SkipEmptyLines     bool
	PassthroughOnEmpty bool
	EnsurePublished    bool
	HandleDoneSignal   bool
	SkipDoneInData     bool
	IdleTimeout        time.Duration
	// Sentinel: enable stream loop detection
	Sentinel *SentinelConfig
}

func GeminiPreprocessor() StreamPreprocessor {
	return func(line []byte) (payload []byte, skip bool) {
		filtered := sseutil.FilterSSEUsageMetadata(line)

		payload = sseutil.JSONPayload(filtered)
		if payload == nil {
			return nil, true
		}

		if !gjson.ValidBytes(payload) {
			log.Debugf("gemini preprocessor: skipping malformed SSE payload")
			return nil, true
		}

		return payload, false
	}
}

func DataTagPreprocessor() StreamPreprocessor {
	return func(line []byte) (payload []byte, skip bool) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			return nil, true
		}

		if bytes.Equal(trimmed, doneMarker) {
			return trimmed, false
		}

		if bytes.HasPrefix(trimmed, dataTag) {
			payload = bytes.TrimSpace(trimmed[len(dataTag):])
		} else {
			payload = trimmed
		}

		if len(payload) == 0 {
			return nil, true
		}

		return payload, false
	}
}

func SendChunk(ctx context.Context, out chan<- provider.StreamChunk, chunk provider.StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

func IsDoneLine(line []byte) bool {
	trimmed := bytes.TrimSpace(line)
	if bytes.Equal(trimmed, doneMarker) {
		return true
	}
	if bytes.HasPrefix(trimmed, dataTag) {
		data := bytes.TrimSpace(trimmed[len(dataTag):])
		return bytes.Equal(data, doneMarker)
	}
	return false
}

func RunSSEStream(
	ctx context.Context,
	body io.ReadCloser,
	reporter UsageReporter,
	processor StreamProcessor,
	cfg StreamConfig,
) <-chan provider.StreamChunk {
	pipeline := streamutil.NewPipeline(ctx, streamutil.PipelineConfig{
		BufferSize: 128,
		OnError: func(err error) {
			log.Errorf("%s: stream error: %v", cfg.ExecutorName, err)
		},
	})

	pipeline.Go(func(ctx context.Context) error {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("%s: panic in stream goroutine: %v", cfg.ExecutorName, r)
			}
		}()

		// Use LineScanner from streamutil with bufio.Reader.ReadSlice for large SSE events
		// Supports lines up to 10MB (vs bufio.Scanner's 64KB initial limit)
		idleTimeout := cfg.IdleTimeout
		if idleTimeout == 0 {
			idleTimeout = DefaultStreamIdleTimeout
		}
		readerCfg := streamutil.StreamReaderConfig{
			IdleTimeout: idleTimeout,
			BufferSize:  256 * 1024,       // 256KB buffer for better streaming throughput
			MaxLineSize: 10 * 1024 * 1024, // 10MB for large SSE events
		}
		scanner := streamutil.NewLineScanner(ctx, body, readerCfg)
		defer scanner.Close()

		// Create sentinel if configured
		var sentinel *StreamSentinel
		if cfg.Sentinel != nil && cfg.Sentinel.Enabled {
			sentinel = NewStreamSentinel(
				WithMaxRepeats(cfg.Sentinel.MaxRepeats),
				WithMaxBytes(cfg.Sentinel.MaxBytes),
				WithWarningThreshold(cfg.Sentinel.WarningThreshold),
				WithOnWarning(func(reason string) {
					log.Warnf("%s: stream sentinel warning: %s", cfg.ExecutorName, reason)
				}),
				WithOnTrigger(func(reason string) {
					log.Warnf("%s: stream sentinel triggered: %s", cfg.ExecutorName, reason)
				}),
			)
		}

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			line := scanner.Bytes()

			// Check Stream Sentinel BEFORE processing
			if sentinel != nil {
				warning, err := sentinel.Check(line)
				if err != nil {
					log.Warnf("Stream sentinel triggered: %v", err)
					return nil
				}
				if warning != nil {
					log.Warnf("Stream sentinel warning: %v", warning)
				}
			}

			if IsDoneLine(line) {
				if cfg.SkipDoneInData {
					continue
				}
				if cfg.HandleDoneSignal && processor != nil {
					doneChunks, doneErr := processor.ProcessDone()
					if doneErr != nil {
						if reporter != nil {
							reporter.PublishFailure(ctx)
						}
						pipeline.SendError(doneErr)
						return nil
					}
					for _, chunk := range doneChunks {
						if !pipeline.SendData(chunk) {
							return nil
						}
					}
				}
				continue
			}

			payload := line
			if cfg.Preprocessor != nil {
				var skip bool
				payload, skip = cfg.Preprocessor(line)
				if skip {
					continue
				}
			}

			if cfg.SkipEmptyLines && len(bytes.TrimSpace(payload)) == 0 {
				continue
			}

			chunks, usage, err := processor.ProcessLine(payload)
			if err != nil {
				if reporter != nil {
					reporter.PublishFailure(ctx)
				}
				if processor != nil {
					if flushed, _ := processor.ProcessDone(); len(flushed) > 0 {
						for _, chunk := range flushed {
							if !pipeline.SendData(chunk) {
								return nil
							}
						}
					}
				}
				errorJSON := fmt.Sprintf("event: error\ndata: {\"type\": \"error\", \"error\": {\"type\": \"server_error\", \"message\": \"%s\"}}\n\n", escapeJSONString(err.Error()))
				pipeline.SendData([]byte(errorJSON))
				return nil
			}

			if usage != nil && reporter != nil {
				reporter.Publish(ctx, usage)
			}

			if len(chunks) > 0 {
				for _, chunk := range chunks {
					if !pipeline.SendData(chunk) {
						return nil
					}
				}
			} else if cfg.PassthroughOnEmpty {
				if !pipeline.SendData(payload) {
					return nil
				}
			}
		}

		if processor != nil {
			doneChunks, doneErr := processor.ProcessDone()
			if doneErr != nil {
				if reporter != nil {
					reporter.PublishFailure(ctx)
				}
				pipeline.SendError(doneErr)
				return nil
			}
			for _, chunk := range doneChunks {
				if !pipeline.SendData(chunk) {
					return nil
				}
			}
		}

		if errScan := scanner.Err(); errScan != nil {
			if reporter != nil {
				reporter.PublishFailure(ctx)
			}
			errorJSON := fmt.Sprintf("event: error\ndata: {\"type\": \"error\", \"error\": {\"type\": \"server_error\", \"message\": \"%s\"}}\n\n", escapeJSONString(errScan.Error()))
			pipeline.SendData([]byte(errorJSON))
			return nil
		}

		if cfg.EnsurePublished && reporter != nil {
			reporter.EnsurePublished(ctx)
		}

		return nil
	})

	pipeline.Start()
	return ConvertPipelineToStreamChunk(ctx, pipeline.Output())
}

type SimpleStreamProcessor struct {
	ProcessFunc func(line []byte) (chunks [][]byte, usage *ir.Usage, err error)
}

func (p *SimpleStreamProcessor) ProcessLine(line []byte) ([][]byte, *ir.Usage, error) {
	if p.ProcessFunc == nil {
		return nil, nil, nil
	}
	return p.ProcessFunc(line)
}

func (p *SimpleStreamProcessor) ProcessDone() ([][]byte, error) {
	return nil, nil
}

func NewSimpleStreamProcessor(fn func(line []byte) (chunks [][]byte, usage *ir.Usage, err error)) *SimpleStreamProcessor {
	return &SimpleStreamProcessor{ProcessFunc: fn}
}

type OpenAIStreamProcessor struct {
	translator *StreamTranslator
	ctx        *StreamContext
	Preprocess func(line []byte, firstChunk bool) []byte
	firstChunk bool
}

func NewOpenAIStreamProcessor(cfg *config.Config, from provider.Format, model, messageID string) *OpenAIStreamProcessor {
	ctx := NewStreamContext()
	return &OpenAIStreamProcessor{
		translator: NewStreamTranslator(cfg, provider.FromString("openai"), from.String(), model, messageID, ctx),
		ctx:        ctx,
		firstChunk: true,
	}
}

func (p *OpenAIStreamProcessor) ProcessLine(line []byte) ([][]byte, *ir.Usage, error) {
	payload := line
	isFirst := p.firstChunk
	if p.Preprocess != nil {
		payload = p.Preprocess(line, isFirst)
		if payload == nil {
			return nil, nil, nil
		}
	}
	p.firstChunk = false

	events, err := to_ir.ParseOpenAIChunk(payload)
	if err != nil {
		return nil, nil, err
	}

	if len(events) == 0 {
		return nil, nil, nil
	}

	result, err := p.translator.Translate(events)
	if err != nil {
		return nil, nil, err
	}
	return result.Chunks, result.Usage, nil
}

func (p *OpenAIStreamProcessor) ProcessDone() ([][]byte, error) {
	events, _ := to_ir.ParseOpenAIChunk([]byte("[DONE]"))
	if len(events) == 0 {
		return p.translator.Flush()
	}
	result, _ := p.translator.Translate(events)
	flushed, err := p.translator.Flush()
	if err != nil {
		return nil, err
	}
	return append(result.Chunks, flushed...), nil
}

// GeminiStreamProcessor processes Gemini SSE stream chunks.
// This is the standard processor for Gemini format responses.
type GeminiStreamProcessor struct {
	translator *StreamTranslator
}

// NewGeminiStreamProcessor creates a processor for Gemini streams.
func NewGeminiStreamProcessor(cfg *config.Config, from provider.Format, model, messageID string, streamCtx *StreamContext) *GeminiStreamProcessor {
	if streamCtx == nil {
		streamCtx = NewStreamContext()
	}
	return &GeminiStreamProcessor{
		translator: NewStreamTranslator(cfg, from, from.String(), model, messageID, streamCtx),
	}
}

func (p *GeminiStreamProcessor) ProcessLine(line []byte) ([][]byte, *ir.Usage, error) {
	events, err := to_ir.ParseGeminiChunkWithStateContext(line, p.translator.Ctx.GeminiState, p.translator.Ctx.ToolSchemaCtx)
	if err != nil {
		return nil, nil, err
	}
	if len(events) == 0 {
		return nil, nil, nil
	}

	result, err := p.translator.Translate(events)
	if err != nil {
		return nil, nil, err
	}
	return result.Chunks, result.Usage, nil
}

func (p *GeminiStreamProcessor) ProcessDone() ([][]byte, error) {
	return p.translator.Flush()
}

func ConvertPipelineToStreamChunk(ctx context.Context, input <-chan streamutil.Chunk) <-chan provider.StreamChunk {
	out := make(chan provider.StreamChunk, 4096) // Single user: maximize channel throughput
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-input:
				if !ok {
					return
				}
				select {
				case out <- provider.StreamChunk{Payload: chunk.Data, Err: chunk.Err}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

func escapeJSONString(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return r.Replace(s)
}

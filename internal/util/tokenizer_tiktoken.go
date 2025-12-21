package util

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/nghyane/llm-mux/internal/translator/ir"
	"github.com/tiktoken-go/tokenizer"
)

var (
	tiktokenCache   = make(map[tokenizer.Encoding]tokenizer.Codec)
	tiktokenCacheMu sync.RWMutex
)

var (
	roleTokenCache   = make(map[string]int64)
	roleTokenCacheMu sync.RWMutex
)

const ImageTokenCostOpenAI = 255

const (
	DocTokenCost   = 500
	AudioTokenCost = 300
	VideoTokenCost = 2000
)

var stringBuilderPool = sync.Pool{
	New: func() any {
		sb := &strings.Builder{}
		sb.Grow(1024)
		return sb
	},
}

// TokenEstimationThreshold is the character count limit above which we switch to estimation
const TokenEstimationThreshold = 100_000

func CountTiktokenTokens(model string, req *ir.UnifiedChatRequest) int64 {
	if req == nil {
		return 0
	}

	encodingName := getTiktokenEncodingName(model)
	enc, err := getTiktokenCodec(encodingName)
	if err != nil {
		return 0
	}

	var totalTokens int64

	const tokensPerMessage int64 = 3

	if req.Instructions != "" {
		totalTokens += countTokens(enc, req.Instructions) + tokensPerMessage
	}

	sb := stringBuilderPool.Get().(*strings.Builder)
	defer stringBuilderPool.Put(sb)

	for i := range req.Messages {
		msg := &req.Messages[i]
		totalTokens += tokensPerMessage
		totalTokens += countRoleTokens(enc, string(msg.Role))

		sb.Reset()
		hasContentToCount := false

		// Process content parts
		// We stream parts into the tokenizer buffer, but switch to direct estimation
		// for large chunks to avoid memory spikes and tokenizer overhead.
		for j := range msg.Content {
			part := &msg.Content[j]
			switch part.Type {
			case ir.ContentTypeText:
				if len(part.Text) > TokenEstimationThreshold {
					if sb.Len() > 0 {
						totalTokens += countTokens(enc, sb.String())
						sb.Reset()
					}
					// Estimate large text directly
					totalTokens += estimateTokens(part.Text)
					hasContentToCount = false
				} else if part.Text != "" {
					sb.WriteString(part.Text)
					hasContentToCount = true
				}

			case ir.ContentTypeReasoning:
				if len(part.Reasoning) > TokenEstimationThreshold {
					if sb.Len() > 0 {
						totalTokens += countTokens(enc, sb.String())
						sb.Reset()
					}
					totalTokens += estimateTokens(part.Reasoning)
					hasContentToCount = false
				} else if part.Reasoning != "" {
					sb.WriteString(part.Reasoning)
					hasContentToCount = true
				}
				if len(part.ThoughtSignature) > 0 {
					sb.Write(part.ThoughtSignature)
					hasContentToCount = true
				}

			case ir.ContentTypeCodeResult:
				if part.CodeExecution != nil && part.CodeExecution.Output != "" {
					if len(part.CodeExecution.Output) > TokenEstimationThreshold {
						if sb.Len() > 0 {
							totalTokens += countTokens(enc, sb.String())
							sb.Reset()
						}
						totalTokens += estimateTokens(part.CodeExecution.Output)
						hasContentToCount = false
					} else {
						sb.WriteString(part.CodeExecution.Output)
						hasContentToCount = true
					}
				}

			case ir.ContentTypeExecutableCode:
				if part.CodeExecution != nil && part.CodeExecution.Code != "" {
					if len(part.CodeExecution.Code) > TokenEstimationThreshold {
						if sb.Len() > 0 {
							totalTokens += countTokens(enc, sb.String())
							sb.Reset()
						}
						totalTokens += estimateTokens(part.CodeExecution.Code)
						hasContentToCount = false
					} else {
						sb.WriteString(part.CodeExecution.Code)
						hasContentToCount = true
					}
				}

			case ir.ContentTypeImage:
				if part.Image != nil {
					totalTokens += ImageTokenCostOpenAI
				}

			case ir.ContentTypeFile:
				if part.File != nil {
					// Handle FileData (potentially huge)
					if part.File.FileData != "" {
						if len(part.File.FileData) > TokenEstimationThreshold {
							if sb.Len() > 0 {
								totalTokens += countTokens(enc, sb.String())
								sb.Reset()
							}
							totalTokens += estimateTokens(part.File.FileData)
							hasContentToCount = false
						} else {
							sb.WriteString(part.File.FileData)
							hasContentToCount = true
						}
					} else if part.File.FileURL != "" || part.File.FileID != "" {
						totalTokens += DocTokenCost
					}
				}

			case ir.ContentTypeAudio:
				if part.Audio != nil {
					if part.Audio.Transcript != "" {
						sb.WriteString(part.Audio.Transcript)
						hasContentToCount = true
					}
					totalTokens += AudioTokenCost
				}

			case ir.ContentTypeVideo:
				if part.Video != nil {
					totalTokens += VideoTokenCost
				}

			case ir.ContentTypeToolResult:
				if part.ToolResult != nil {
					// Tool result formatting involves mixed content, handle carefully
					// Simplified: just estimate if result is huge
					if len(part.ToolResult.Result) > TokenEstimationThreshold {
						if sb.Len() > 0 {
							totalTokens += countTokens(enc, sb.String())
							sb.Reset()
						}
						sb.WriteString("\nTool ")
						sb.WriteString(part.ToolResult.ToolCallID)
						sb.WriteString(" result: ")
						// Flush header
						totalTokens += countTokens(enc, sb.String())
						sb.Reset()

						totalTokens += estimateTokens(part.ToolResult.Result)
						hasContentToCount = false
					} else {
						sb.WriteString("\nTool ")
						sb.WriteString(part.ToolResult.ToolCallID)
						sb.WriteString(" result: ")
						sb.WriteString(part.ToolResult.Result)
						hasContentToCount = true
					}
					totalTokens += int64(len(part.ToolResult.Images) * ImageTokenCostOpenAI)
				}
			}
		}

		// Process tool calls
		for j := range msg.ToolCalls {
			tc := &msg.ToolCalls[j]
			sb.WriteString("\nCall tool ")
			sb.WriteString(tc.Name)
			sb.WriteByte('(')
			if len(tc.Args) > TokenEstimationThreshold {
				// Flush prefix
				totalTokens += countTokens(enc, sb.String())
				sb.Reset()

				totalTokens += estimateTokens(tc.Args)
				sb.WriteByte(')')
				// Flush suffix
				totalTokens += countTokens(enc, sb.String())
				sb.Reset()
				hasContentToCount = false
			} else {
				sb.WriteString(tc.Args)
				sb.WriteByte(')')
				hasContentToCount = true
			}
		}

		// Final flush for this message
		if hasContentToCount && sb.Len() > 0 {
			totalTokens += countTokens(enc, sb.String())
		}

	}

	return totalTokens
}

// estimateTokens returns a fast approximation of token count for large strings
// Uses divisor 3.5 to approximate common token densities without overhead
func estimateTokens(s string) int64 {
	return int64(float64(len(s)) / 3.5)
}

func countTokens(enc tokenizer.Codec, s string) int64 {
	// Only use estimation for very large strings to avoid memory issues
	if len(s) > TokenEstimationThreshold { // Use same threshold as everywhere else
		return estimateTokens(s)
	}
	ids, _, _ := enc.Encode(s)
	return int64(len(ids))
}

func countJSONTokens(enc tokenizer.Codec, v any) int64 {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	if len(data) > TokenEstimationThreshold {
		// Use higher divisor for JSON as it has structural overhead (quotes, braces)
		// that inflates char count but often tokenizes efficiently
		return int64(float64(len(data)) / 4.0)
	}
	ids, _, _ := enc.Encode(string(data))
	return int64(len(ids))
}

func countRoleTokens(enc tokenizer.Codec, role string) int64 {
	roleTokenCacheMu.RLock()
	count, ok := roleTokenCache[role]
	roleTokenCacheMu.RUnlock()
	if ok {
		return count
	}

	ids, _, _ := enc.Encode(role)
	count = int64(len(ids))

	roleTokenCacheMu.Lock()
	roleTokenCache[role] = count
	roleTokenCacheMu.Unlock()

	return count
}

func getTiktokenCodec(encoding tokenizer.Encoding) (tokenizer.Codec, error) {
	tiktokenCacheMu.RLock()
	codec, ok := tiktokenCache[encoding]
	tiktokenCacheMu.RUnlock()
	if ok {
		return codec, nil
	}

	tiktokenCacheMu.Lock()
	defer tiktokenCacheMu.Unlock()

	if codec, ok := tiktokenCache[encoding]; ok {
		return codec, nil
	}

	codec, err := tokenizer.Get(encoding)
	if err != nil {
		return nil, err
	}

	tiktokenCache[encoding] = codec
	return codec, nil
}

func getTiktokenEncodingName(model string) tokenizer.Encoding {
	lower := strings.ToLower(model)

	switch {
	case strings.Contains(lower, "gpt-5"),
		strings.Contains(lower, "gpt-4o"),
		strings.Contains(lower, "claude"),
		strings.Contains(lower, "qwen"),
		strings.Contains(lower, "antigravity"):
		return tokenizer.O200kBase

	case strings.Contains(lower, "gpt-4"),
		strings.Contains(lower, "gpt-3.5"),
		strings.Contains(lower, "turbo"):
		return tokenizer.Cl100kBase

	default:
		return tokenizer.O200kBase
	}
}

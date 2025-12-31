package from_ir

import (
	"encoding/json"
	"testing"

	"github.com/nghyane/llm-mux/internal/translator/ir"
	"github.com/tidwall/gjson"
)

func TestVertexEnvelopeProvider_ClaudeCacheControl(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}},
				CacheControl: &ir.CacheControl{
					Type: "ephemeral",
				},
			},
		},
		MaxTokens: ir.Ptr(1024),
	}

	p := &VertexEnvelopeProvider{}
	payload, err := p.ConvertRequest(req)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	parsed := gjson.ParseBytes(payload)

	if !parsed.Get("model").Exists() {
		t.Error("model field missing")
	}
	if parsed.Get("model").String() != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", parsed.Get("model").String(), "claude-sonnet-4-20250514")
	}

	contents := parsed.Get("request.contents")
	if !contents.IsArray() {
		t.Fatal("request.contents should be an array")
	}
	if len(contents.Array()) == 0 {
		t.Fatal("request.contents should have at least one item")
	}

	userContent := contents.Array()[0]
	cacheControl := userContent.Get("cacheControl")
	if !cacheControl.Exists() {
		t.Fatal("cacheControl should exist in user content")
	}
	if cacheControl.Get("type").String() != "ephemeral" {
		t.Errorf("cacheControl.type = %q, want %q", cacheControl.Get("type").String(), "ephemeral")
	}
}

func TestVertexEnvelopeProvider_ClaudeCacheControlWithTTL(t *testing.T) {
	ttl := int64(3600)
	req := &ir.UnifiedChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}},
				CacheControl: &ir.CacheControl{
					Type: "ephemeral",
					TTL:  &ttl,
				},
			},
		},
		MaxTokens: ir.Ptr(1024),
	}

	p := &VertexEnvelopeProvider{}
	payload, err := p.ConvertRequest(req)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	parsed := gjson.ParseBytes(payload)
	cacheControl := parsed.Get("request.contents.0.cacheControl")

	if !cacheControl.Exists() {
		t.Fatal("cacheControl should exist")
	}
	if cacheControl.Get("type").String() != "ephemeral" {
		t.Errorf("cacheControl.type = %q, want %q", cacheControl.Get("type").String(), "ephemeral")
	}
	if cacheControl.Get("ttl").Int() != 3600 {
		t.Errorf("cacheControl.ttl = %d, want %d", cacheControl.Get("ttl").Int(), 3600)
	}
}

func TestVertexEnvelopeProvider_ClaudeNoCacheControl(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}},
			},
		},
		MaxTokens: ir.Ptr(1024),
	}

	p := &VertexEnvelopeProvider{}
	payload, err := p.ConvertRequest(req)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	parsed := gjson.ParseBytes(payload)
	cacheControl := parsed.Get("request.contents.0.cacheControl")

	if cacheControl.Exists() {
		t.Error("cacheControl should not exist when not set")
	}
}

func TestVertexEnvelopeProvider_ClaudeMultipleMessagesWithCacheControl(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ir.Message{
			{
				Role:    ir.RoleSystem,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "You are a helpful assistant."}},
			},
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Context: ..."}},
				CacheControl: &ir.CacheControl{
					Type: "ephemeral",
				},
			},
			{
				Role:    ir.RoleAssistant,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "I understand."}},
			},
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Question?"}},
			},
		},
		MaxTokens: ir.Ptr(1024),
	}

	p := &VertexEnvelopeProvider{}
	payload, err := p.ConvertRequest(req)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	parsed := gjson.ParseBytes(payload)

	sysInstruction := parsed.Get("request.systemInstruction")
	if !sysInstruction.Exists() {
		t.Error("systemInstruction should exist")
	}

	contents := parsed.Get("request.contents").Array()
	if len(contents) != 3 {
		t.Fatalf("expected 3 content items (user, model, user), got %d", len(contents))
	}

	if !contents[0].Get("cacheControl").Exists() {
		t.Error("first user message should have cacheControl")
	}
	if contents[2].Get("cacheControl").Exists() {
		t.Error("last user message should not have cacheControl")
	}
}

func TestVertexEnvelopeProvider_ClaudeCoalescesConsecutiveSameRoleMessages(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Part 1"}},
			},
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Part 2"}},
			},
			{
				Role:    ir.RoleAssistant,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Response"}},
			},
			{
				Role: ir.RoleTool,
				Content: []ir.ContentPart{
					{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_1", Result: "result"}},
				},
			},
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Follow up"}},
			},
		},
		MaxTokens: ir.Ptr(1024),
	}

	p := &VertexEnvelopeProvider{}
	payload, err := p.ConvertRequest(req)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	parsed := gjson.ParseBytes(payload)
	contents := parsed.Get("request.contents").Array()

	if len(contents) != 3 {
		t.Fatalf("expected 3 coalesced messages (user, model, user), got %d", len(contents))
	}

	if contents[0].Get("role").String() != "user" {
		t.Error("first message should be user")
	}
	if contents[1].Get("role").String() != "model" {
		t.Error("second message should be model")
	}
	if contents[2].Get("role").String() != "user" {
		t.Error("third message should be user (RoleTool + RoleUser coalesced)")
	}

	firstUserParts := contents[0].Get("parts").Array()
	if len(firstUserParts) != 2 {
		t.Errorf("first user message should have 2 parts (coalesced), got %d", len(firstUserParts))
	}

	thirdUserParts := contents[2].Get("parts").Array()
	if len(thirdUserParts) != 2 {
		t.Errorf("third user message should have 2 parts (functionResponse + text), got %d", len(thirdUserParts))
	}
}

func TestVertexEnvelopeProvider_ClaudeThinkingConfig(t *testing.T) {
	budget := int32(4096)
	req := &ir.UnifiedChatRequest{
		Model: "claude-sonnet-4-20250514-thinking",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}},
			},
		},
		MaxTokens: ir.Ptr(8192),
		Thinking: &ir.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingBudget:  &budget,
		},
	}

	p := &VertexEnvelopeProvider{}
	payload, err := p.ConvertRequest(req)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	parsed := gjson.ParseBytes(payload)
	thinkingConfig := parsed.Get("request.generationConfig.thinkingConfig")

	if !thinkingConfig.Exists() {
		t.Fatal("thinkingConfig should exist")
	}
	if !thinkingConfig.Get("includeThoughts").Bool() {
		t.Error("includeThoughts should be true")
	}
	if thinkingConfig.Get("thinkingBudget").Int() != 4096 {
		t.Errorf("thinkingBudget = %d, want 4096", thinkingConfig.Get("thinkingBudget").Int())
	}
}

func TestVertexEnvelopeProvider_ClaudeTools(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}},
			},
		},
		Tools: []ir.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get weather for a location",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{"type": "string"},
					},
				},
			},
		},
		MaxTokens: ir.Ptr(1024),
	}

	p := &VertexEnvelopeProvider{}
	payload, err := p.ConvertRequest(req)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	parsed := gjson.ParseBytes(payload)
	tools := parsed.Get("request.tools")

	if !tools.IsArray() {
		t.Fatal("tools should be an array")
	}

	funcDecls := tools.Array()[0].Get("functionDeclarations")
	if !funcDecls.IsArray() {
		t.Fatal("functionDeclarations should be an array")
	}
	if funcDecls.Array()[0].Get("name").String() != "get_weather" {
		t.Error("function name should be get_weather")
	}
}

func TestVertexEnvelopeProvider_EnvelopeStructure(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}},
			},
		},
		MaxTokens: ir.Ptr(1024),
	}

	p := &VertexEnvelopeProvider{}
	payload, err := p.ConvertRequest(req)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("Failed to unmarshal envelope: %v", err)
	}

	if _, ok := envelope["project"]; !ok {
		t.Error("envelope should have 'project' field")
	}
	if _, ok := envelope["model"]; !ok {
		t.Error("envelope should have 'model' field")
	}
	if _, ok := envelope["request"]; !ok {
		t.Error("envelope should have 'request' field")
	}
}

func TestVertexEnvelopeProvider_GeminiModel(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Model: "gemini-2.5-pro",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}},
			},
		},
		MaxTokens: ir.Ptr(1024),
	}

	p := &VertexEnvelopeProvider{}
	payload, err := p.ConvertRequest(req)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	parsed := gjson.ParseBytes(payload)

	if parsed.Get("model").String() != "gemini-2.5-pro" {
		t.Errorf("model = %q, want %q", parsed.Get("model").String(), "gemini-2.5-pro")
	}
	if !parsed.Get("request.contents").Exists() {
		t.Error("request.contents should exist for Gemini model")
	}
}

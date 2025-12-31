package preprocess

import (
	"testing"

	"github.com/nghyane/llm-mux/internal/translator/ir"
)

func TestToolValidation_LastMessageToolUse(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Use a tool"}}},
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{{ID: "call_1", Name: "search", Args: "{}"}}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages) != 2 {
		t.Errorf("Expected 2 messages (last message tool_use preserved), got %d", len(req.Messages))
	}
	if len(req.Messages[1].ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call in last message, got %d", len(req.Messages[1].ToolCalls))
	}
}

func TestToolValidation_PartialResults(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Use tools"}}},
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{
				{ID: "call_1", Name: "search", Args: "{}"},
				{ID: "call_2", Name: "calc", Args: "{}"},
				{ID: "call_3", Name: "write", Args: "{}"},
			}},
			{Role: ir.RoleUser, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_1", Result: "found"}},
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_3", Result: "written"}},
			}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages[1].ToolCalls) != 2 {
		t.Errorf("Expected 2 tool calls (call_1, call_3), got %d", len(req.Messages[1].ToolCalls))
	}
	if len(req.Messages[2].Content) != 2 {
		t.Errorf("Expected 2 tool results, got %d", len(req.Messages[2].Content))
	}
}

func TestToolValidation_MixedUserContent(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}}},
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{{ID: "call_1", Name: "search", Args: "{}"}}},
			{Role: ir.RoleUser, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_1", Result: "result"}},
				{Type: ir.ContentTypeText, Text: "Great, now do something else"},
			}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages[2].Content) != 2 {
		t.Errorf("Expected 2 content parts (tool_result + text), got %d", len(req.Messages[2].Content))
	}
}

func TestToolValidation_ToolRoleMessage(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}}},
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{{ID: "call_1", Name: "search", Args: "{}"}}},
			{Role: ir.RoleTool, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_1", Result: "result"}},
			}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages[1].ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(req.Messages[1].ToolCalls))
	}
	if len(req.Messages[2].Content) != 1 {
		t.Errorf("Expected 1 content part, got %d", len(req.Messages[2].Content))
	}
}

func TestToolValidation_WrongPosition(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{{ID: "call_1", Name: "search", Args: "{}"}}},
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hey"}}},
			{Role: ir.RoleUser, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_1", Result: "result"}},
			}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages[0].ToolCalls) != 0 {
		t.Errorf("Expected 0 tool calls (not immediately followed), got %d", len(req.Messages[0].ToolCalls))
	}
}

func TestToolValidation_ConsecutiveAssistantToolUse(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{{ID: "call_1", Name: "search", Args: "{}"}}},
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{{ID: "call_2", Name: "calc", Args: "{}"}}},
			{Role: ir.RoleUser, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_2", Result: "result2"}},
			}},
		},
	}
	applyToolValidation(req)

	// First assistant (call_1) is invalid (next is assistant, not user/tool) → becomes empty → removed
	// Second assistant (call_2) is valid → kept
	// After cleanup: [assistant with call_2, user with result]
	if len(req.Messages) != 2 {
		t.Fatalf("Expected 2 messages after cleanup, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != ir.RoleAssistant {
		t.Errorf("Expected first message to be assistant, got %s", req.Messages[0].Role)
	}
	if len(req.Messages[0].ToolCalls) != 1 || req.Messages[0].ToolCalls[0].ID != "call_2" {
		t.Errorf("Expected assistant to have call_2, got %v", req.Messages[0].ToolCalls)
	}
}

func TestToolValidation_EmptyMessageRemoval(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hi"}}},
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{{ID: "call_1", Name: "search", Args: "{}"}}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages) != 2 {
		t.Errorf("Expected 2 messages (last message tool_use preserved), got %d", len(req.Messages))
	}
}

func TestToolValidation_ValidSequence(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}}},
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{{ID: "call_1", Name: "search", Args: "{}"}}},
			{Role: ir.RoleUser, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_1", Result: "result"}},
			}},
			{Role: ir.RoleAssistant, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Done"}}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages) != 4 {
		t.Errorf("Expected 4 messages (valid sequence unchanged), got %d", len(req.Messages))
	}
	if len(req.Messages[1].ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(req.Messages[1].ToolCalls))
	}
}

func TestToolValidation_MultipleToolUseMultipleResults(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Do tasks"}}},
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{
				{ID: "call_a", Name: "task_a", Args: "{}"},
				{ID: "call_b", Name: "task_b", Args: "{}"},
			}},
			{Role: ir.RoleUser, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_a", Result: "a done"}},
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_b", Result: "b done"}},
			}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages[1].ToolCalls) != 2 {
		t.Errorf("Expected 2 tool calls (both valid), got %d", len(req.Messages[1].ToolCalls))
	}
	if len(req.Messages[2].Content) != 2 {
		t.Errorf("Expected 2 tool results, got %d", len(req.Messages[2].Content))
	}
}

func TestToolValidation_OrphanedToolResultOnly(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hi"}}},
			{Role: ir.RoleUser, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "nonexistent", Result: "orphan"}},
			}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages) != 1 {
		t.Errorf("Expected 1 message after removing orphaned result, got %d", len(req.Messages))
	}
}

func TestToolValidation_NoToolsAtAll(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hello"}}},
			{Role: ir.RoleAssistant, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Hi there"}}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages) != 2 {
		t.Errorf("Expected 2 messages (no changes), got %d", len(req.Messages))
	}
}

func TestToolValidation_EmptyMessages(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{},
	}
	applyToolValidation(req)

	if len(req.Messages) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(req.Messages))
	}
}

func TestToolValidation_ConsecutiveToolRoleMessages(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleAssistant, ToolCalls: []ir.ToolCall{
				{ID: "call_1", Name: "search", Args: "{}"},
				{ID: "call_2", Name: "calc", Args: "{}"},
			}},
			{Role: ir.RoleTool, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_1", Result: "result1"}},
			}},
			{Role: ir.RoleTool, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_2", Result: "result2"}},
			}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages) != 2 {
		t.Fatalf("Expected 2 messages (call_2 invalid - not immediately after), got %d", len(req.Messages))
	}
	if len(req.Messages[0].ToolCalls) != 1 || req.Messages[0].ToolCalls[0].ID != "call_1" {
		t.Errorf("Expected only call_1 in assistant, got %v", req.Messages[0].ToolCalls)
	}
	if len(req.Messages[1].Content) != 1 {
		t.Errorf("Expected 1 tool result, got %d", len(req.Messages[1].Content))
	}
}

func TestToolValidation_AssistantWithTextAndToolCalls(t *testing.T) {
	req := &ir.UnifiedChatRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Search"}}},
			{
				Role:      ir.RoleAssistant,
				Content:   []ir.ContentPart{{Type: ir.ContentTypeText, Text: "Let me search for that"}},
				ToolCalls: []ir.ToolCall{{ID: "call_1", Name: "search", Args: "{}"}},
			},
			{Role: ir.RoleUser, Content: []ir.ContentPart{
				{Type: ir.ContentTypeToolResult, ToolResult: &ir.ToolResultPart{ToolCallID: "call_1", Result: "found"}},
			}},
		},
	}
	applyToolValidation(req)

	if len(req.Messages[1].ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call (valid), got %d", len(req.Messages[1].ToolCalls))
	}
	if len(req.Messages[1].Content) != 1 {
		t.Errorf("Expected 1 text content, got %d", len(req.Messages[1].Content))
	}
}

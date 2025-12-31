package preprocess

import (
	"github.com/nghyane/llm-mux/internal/translator/ir"
)

// applyToolValidation validates and cleans tool_use/tool_result pairs.
// Claude API requires that each tool_use must have a corresponding tool_result
// in the IMMEDIATELY following message. This function removes invalid pairs.
func applyToolValidation(req *ir.UnifiedChatRequest) {
	if len(req.Messages) == 0 {
		return
	}

	// Phase 1: Build map of valid (tool_use_id → assistant_message_index) pairs
	// A pair is valid only if tool_result is in the message immediately after tool_use
	validPairs := validateToolPairs(req.Messages)

	// Phase 2: Remove tool_use blocks without immediate results
	removeInvalidToolCalls(req, validPairs)

	// Phase 3: Remove orphaned tool_results
	removeOrphanedToolResults(req, validPairs)

	// Phase 4: Remove empty messages
	removeEmptyMessages(req)
}

// validateToolPairs identifies which tool_use/tool_result pairs are valid.
// Returns map of tool_use_id → index of assistant message that has it.
// A pair is valid if:
// 1. The tool_result is in the IMMEDIATELY following message, OR
// 2. The tool_use is in the LAST message (pending response from model)
func validateToolPairs(messages []ir.Message) map[string]int {
	validPairs := make(map[string]int)
	lastIdx := len(messages) - 1

	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		// Only check assistant messages with tool calls
		if msg.Role != ir.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}

		// Collect tool_use IDs from this message
		toolUseIDs := make(map[string]struct{})
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" {
				toolUseIDs[tc.ID] = struct{}{}
			}
		}

		// Special case: Last message with tool_use is always valid
		// This happens when we're sending a new request with pending tool calls
		// The model is expected to provide tool_result in its response
		if i == lastIdx {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					validPairs[tc.ID] = i
				}
			}
			continue
		}

		nextMsg := messages[i+1]

		// The next message must be user or tool role to contain tool_results
		if nextMsg.Role != ir.RoleUser && nextMsg.Role != ir.RoleTool {
			// Next message is not a tool result carrier - all tool_use are invalid
			continue
		}

		// Collect tool_result IDs from the next message
		for _, part := range nextMsg.Content {
			if part.Type == ir.ContentTypeToolResult && part.ToolResult != nil {
				callID := part.ToolResult.ToolCallID
				// Only valid if the tool_use exists in the PREVIOUS assistant message
				if _, exists := toolUseIDs[callID]; exists {
					validPairs[callID] = i
				}
			}
		}
	}

	return validPairs
}

// removeInvalidToolCalls removes tool_use blocks that don't have immediate results.
func removeInvalidToolCalls(req *ir.UnifiedChatRequest, validPairs map[string]int) {
	for i := range req.Messages {
		msg := &req.Messages[i]

		if msg.Role != ir.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}

		filtered := msg.ToolCalls[:0] // reuse backing array
		for _, tc := range msg.ToolCalls {
			// Keep only if this tool_use ID is in validPairs AND originated from this message
			if idx, exists := validPairs[tc.ID]; exists && idx == i {
				filtered = append(filtered, tc)
			}
		}
		msg.ToolCalls = filtered
	}
}

// removeOrphanedToolResults removes tool_results without valid tool_use in the preceding message.
func removeOrphanedToolResults(req *ir.UnifiedChatRequest, validPairs map[string]int) {
	for i := range req.Messages {
		msg := &req.Messages[i]

		if msg.Role != ir.RoleTool && msg.Role != ir.RoleUser {
			continue
		}

		filtered := make([]ir.ContentPart, 0, len(msg.Content))
		for _, part := range msg.Content {
			if part.Type == ir.ContentTypeToolResult && part.ToolResult != nil {
				callID := part.ToolResult.ToolCallID
				// Keep only if valid AND the tool_use was in the PREVIOUS message
				if idx, exists := validPairs[callID]; exists && idx == i-1 {
					filtered = append(filtered, part)
				}
				// Otherwise, skip (orphaned)
			} else {
				// Keep all non-tool-result content (text, images, etc.)
				filtered = append(filtered, part)
			}
		}
		msg.Content = filtered
	}
}

// removeEmptyMessages removes messages that became empty after cleanup.
func removeEmptyMessages(req *ir.UnifiedChatRequest) {
	filtered := make([]ir.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if isEmptyMessage(msg) {
			continue
		}
		filtered = append(filtered, msg)
	}
	req.Messages = filtered
}

// isEmptyMessage checks if a message has no meaningful content.
func isEmptyMessage(msg ir.Message) bool {
	// Has tool calls - not empty
	if len(msg.ToolCalls) > 0 {
		return false
	}

	// Check content parts
	for _, part := range msg.Content {
		switch part.Type {
		case ir.ContentTypeText:
			if part.Text != "" {
				return false
			}
		default:
			// Any non-text content type means not empty
			return false
		}
	}

	return true
}

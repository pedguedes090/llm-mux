// Package preprocess handles IR normalization before translation.
// This separates business logic from format conversion.
package preprocess

import (
	"github.com/nghyane/llm-mux/internal/registry"
	"github.com/nghyane/llm-mux/internal/translator/ir"
)

// Apply normalizes the IR request before translation.
// This is the single entry point for all preprocessing.
func Apply(req *ir.UnifiedChatRequest) error {
	if req == nil {
		return nil
	}

	info := registry.GetGlobalRegistry().GetModelInfo(req.Model)

	// Apply in order: thinking → limits → defaults → tools
	applyThinkingNormalization(req, info)
	applyLimits(req, info)
	applyProviderDefaults(req, info)
	applyToolValidation(req)

	return nil
}

package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nghyane/llm-mux/internal/config"
	"github.com/nghyane/llm-mux/internal/json"
	"github.com/nghyane/llm-mux/internal/provider"
)

// TestApplySystemInstructionWithAntigravityIdentity tests the system instruction logic.
func TestApplySystemInstructionWithAntigravityIdentity(t *testing.T) {
	tests := []struct {
		name           string
		inputPayload   string
		shouldKeepAsIs bool // true = user already has Antigravity identity, should not modify
		expectContains string
	}{
		{
			name:           "empty payload - should inject fallback",
			inputPayload:   `{"request":{}}`,
			shouldKeepAsIs: false,
			expectContains: "You are Antigravity",
		},
		{
			name:           "has systemInstruction without Antigravity - should inject",
			inputPayload:   `{"request":{"systemInstruction":{"parts":[{"text":"You are a helpful assistant"}]}}}`,
			shouldKeepAsIs: false,
			expectContains: "You are Antigravity",
		},
		{
			name:           "already has Antigravity identity - should keep as-is",
			inputPayload:   `{"request":{"systemInstruction":{"parts":[{"text":"You are Antigravity, a powerful AI..."}]}}}`,
			shouldKeepAsIs: true,
			expectContains: "You are Antigravity, a powerful AI",
		},
		{
			name:           "has Antigravity in second part - should keep as-is",
			inputPayload:   `{"request":{"systemInstruction":{"parts":[{"text":"Part 1"},{"text":"You are Antigravity in part 2"}]}}}`,
			shouldKeepAsIs: true,
			expectContains: "Part 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applySystemInstructionWithAntigravityIdentity([]byte(tt.inputPayload))
			resultStr := string(result)

			if !strings.Contains(resultStr, tt.expectContains) {
				t.Errorf("expected result to contain %q, got: %s", tt.expectContains, resultStr)
			}

			// If we expected to keep as-is, verify it wasn't modified significantly
			if tt.shouldKeepAsIs {
				// The result should still contain user's original content
				if !strings.Contains(resultStr, tt.expectContains) {
					t.Errorf("expected to keep user content %q, but got: %s", tt.expectContains, resultStr)
				}
			} else {
				// The result should contain the injected identity
				if !strings.Contains(resultStr, "Google Deepmind") {
					t.Errorf("expected fallback identity to contain Google Deepmind, got: %s", resultStr)
				}
			}

			t.Logf("Result: %s", resultStr)
		})
	}
}

// TestAntigravityExecutorWithAuthFile tests the executor using existing auth files.
// This test reads from ~/.config/llm-mux/auth/ to find an antigravity auth file.
func TestAntigravityExecutorWithAuthFile(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run")
	}

	// Find auth dir
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}

	authDir := filepath.Join(homeDir, ".config", "llm-mux", "auth")
	entries, err := os.ReadDir(authDir)
	if err != nil {
		t.Skipf("Auth dir not found: %s", authDir)
	}

	// Find first antigravity auth file
	var authData map[string]any
	var foundAuthFile string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// Check filename prefix first
		if !strings.HasPrefix(entry.Name(), "antigravity-") && !strings.HasPrefix(entry.Name(), "gemini-cli-") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(authDir, entry.Name()))
		if err != nil {
			continue
		}

		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			continue
		}

		// Check 'type' field (auth file format uses 'type', not 'provider')
		if authType, ok := parsed["type"].(string); ok {
			if authType == "antigravity" || authType == "gemini-cli" {
				authData = parsed
				foundAuthFile = entry.Name()
				break
			}
		}
	}

	if authData == nil {
		t.Skip("No antigravity/gemini-cli auth file found")
	}

	t.Logf("Using auth file: %s", foundAuthFile)

	// Extract access_token (auth file has token at root level, not in metadata)
	accessToken, _ := authData["access_token"].(string)
	if accessToken == "" {
		// Try metadata as fallback
		if meta, ok := authData["metadata"].(map[string]any); ok {
			accessToken, _ = meta["access_token"].(string)
		}
	}

	if accessToken == "" {
		t.Skip("No access_token in auth file")
	}

	// Build metadata for provider.Auth (copy all root fields to metadata)
	metadata := make(map[string]any)
	for k, v := range authData {
		metadata[k] = v
	}
	// Ensure timestamp is set
	if _, ok := metadata["timestamp"]; !ok {
		metadata["timestamp"] = time.Now().UnixMilli()
	}
	if _, ok := metadata["expires_in"]; !ok {
		metadata["expires_in"] = int64(3600)
	}

	auth := &provider.Auth{
		ID:       foundAuthFile,
		Provider: "antigravity",
		Metadata: metadata,
	}

	cfg := &config.Config{}
	executor := NewAntigravityExecutor(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Refresh token first (in case it's expired)
	refreshedAuth, err := executor.Refresh(ctx, auth)
	if err != nil {
		t.Logf("Token refresh failed (may be expired): %v", err)
		// Continue with original auth - might still work
	} else if refreshedAuth != nil {
		auth = refreshedAuth
		t.Logf("Token refreshed successfully")
	}

	payload := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [
			{"role": "user", "content": "Say hi and tell me your name in one sentence"}
		],
		"max_tokens": 100
	}`)

	req := provider.Request{
		Model:   "gemini-2.5-flash",
		Payload: payload,
	}

	opts := provider.Options{
		SourceFormat: provider.FormatOpenAI,
	}

	resp, err := executor.Execute(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	t.Logf("Response length: %d bytes", len(resp.Payload))
	t.Logf("Response: %s", string(resp.Payload))

	if len(resp.Payload) == 0 {
		t.Error("Expected non-empty response")
	}

	// Check if response contains expected structure (OpenAI format)
	if !strings.Contains(string(resp.Payload), "choices") {
		t.Logf("Note: Response may be in Gemini format, not OpenAI format")
	}
}

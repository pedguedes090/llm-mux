package watcher

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/nghyane/llm-mux/internal/config"
)

// buildConfigChangeDetails computes a redacted, human-readable list of config changes.
// It avoids printing secrets (like API keys) and focuses on structural or non-sensitive fields.
func buildConfigChangeDetails(oldCfg, newCfg *config.Config) []string {
	changes := make([]string, 0, 16)
	if oldCfg == nil || newCfg == nil {
		return changes
	}

	// Simple scalars
	if oldCfg.Port != newCfg.Port {
		changes = append(changes, fmt.Sprintf("port: %d -> %d", oldCfg.Port, newCfg.Port))
	}
	if oldCfg.AuthDir != newCfg.AuthDir {
		changes = append(changes, fmt.Sprintf("auth-dir: %s -> %s", oldCfg.AuthDir, newCfg.AuthDir))
	}
	if oldCfg.Debug != newCfg.Debug {
		changes = append(changes, fmt.Sprintf("debug: %t -> %t", oldCfg.Debug, newCfg.Debug))
	}
	if oldCfg.LoggingToFile != newCfg.LoggingToFile {
		changes = append(changes, fmt.Sprintf("logging-to-file: %t -> %t", oldCfg.LoggingToFile, newCfg.LoggingToFile))
	}
	if oldCfg.UsageStatisticsEnabled != newCfg.UsageStatisticsEnabled {
		changes = append(changes, fmt.Sprintf("usage-statistics-enabled: %t -> %t", oldCfg.UsageStatisticsEnabled, newCfg.UsageStatisticsEnabled))
	}
	if oldCfg.DisableCooling != newCfg.DisableCooling {
		changes = append(changes, fmt.Sprintf("disable-cooling: %t -> %t", oldCfg.DisableCooling, newCfg.DisableCooling))
	}
	if oldCfg.RequestLog != newCfg.RequestLog {
		changes = append(changes, fmt.Sprintf("request-log: %t -> %t", oldCfg.RequestLog, newCfg.RequestLog))
	}
	if oldCfg.RequestRetry != newCfg.RequestRetry {
		changes = append(changes, fmt.Sprintf("request-retry: %d -> %d", oldCfg.RequestRetry, newCfg.RequestRetry))
	}
	if oldCfg.MaxRetryInterval != newCfg.MaxRetryInterval {
		changes = append(changes, fmt.Sprintf("max-retry-interval: %d -> %d", oldCfg.MaxRetryInterval, newCfg.MaxRetryInterval))
	}
	if oldCfg.ProxyURL != newCfg.ProxyURL {
		changes = append(changes, fmt.Sprintf("proxy-url: %s -> %s", oldCfg.ProxyURL, newCfg.ProxyURL))
	}
	if oldCfg.WebsocketAuth != newCfg.WebsocketAuth {
		changes = append(changes, fmt.Sprintf("ws-auth: %t -> %t", oldCfg.WebsocketAuth, newCfg.WebsocketAuth))
	}

	// Quota-exceeded behavior
	if oldCfg.QuotaExceeded.SwitchProject != newCfg.QuotaExceeded.SwitchProject {
		changes = append(changes, fmt.Sprintf("quota-exceeded.switch-project: %t -> %t", oldCfg.QuotaExceeded.SwitchProject, newCfg.QuotaExceeded.SwitchProject))
	}
	if oldCfg.QuotaExceeded.SwitchPreviewModel != newCfg.QuotaExceeded.SwitchPreviewModel {
		changes = append(changes, fmt.Sprintf("quota-exceeded.switch-preview-model: %t -> %t", oldCfg.QuotaExceeded.SwitchPreviewModel, newCfg.QuotaExceeded.SwitchPreviewModel))
	}

	// API keys (redacted) and counts
	if len(oldCfg.APIKeys) != len(newCfg.APIKeys) {
		changes = append(changes, fmt.Sprintf("api-keys count: %d -> %d", len(oldCfg.APIKeys), len(newCfg.APIKeys)))
	} else if !reflect.DeepEqual(trimStrings(oldCfg.APIKeys), trimStrings(newCfg.APIKeys)) {
		changes = append(changes, "api-keys: values updated (count unchanged, redacted)")
	}
	if len(oldCfg.GeminiKey) != len(newCfg.GeminiKey) {
		changes = append(changes, fmt.Sprintf("gemini-api-key count: %d -> %d", len(oldCfg.GeminiKey), len(newCfg.GeminiKey)))
	} else {
		for i := range oldCfg.GeminiKey {
			if i >= len(newCfg.GeminiKey) {
				break
			}
			o := oldCfg.GeminiKey[i]
			n := newCfg.GeminiKey[i]
			if strings.TrimSpace(o.BaseURL) != strings.TrimSpace(n.BaseURL) {
				changes = append(changes, fmt.Sprintf("gemini[%d].base-url: %s -> %s", i, strings.TrimSpace(o.BaseURL), strings.TrimSpace(n.BaseURL)))
			}
			if strings.TrimSpace(o.ProxyURL) != strings.TrimSpace(n.ProxyURL) {
				changes = append(changes, fmt.Sprintf("gemini[%d].proxy-url: %s -> %s", i, strings.TrimSpace(o.ProxyURL), strings.TrimSpace(n.ProxyURL)))
			}
			if strings.TrimSpace(o.APIKey) != strings.TrimSpace(n.APIKey) {
				changes = append(changes, fmt.Sprintf("gemini[%d].api-key: updated", i))
			}
			if !equalStringMap(o.Headers, n.Headers) {
				changes = append(changes, fmt.Sprintf("gemini[%d].headers: updated", i))
			}
			oldExcluded := summarizeExcludedModels(o.ExcludedModels)
			newExcluded := summarizeExcludedModels(n.ExcludedModels)
			if oldExcluded.hash != newExcluded.hash {
				changes = append(changes, fmt.Sprintf("gemini[%d].excluded-models: updated (%d -> %d entries)", i, oldExcluded.count, newExcluded.count))
			}
		}
	}

	// Claude keys (do not print key material)
	if len(oldCfg.ClaudeKey) != len(newCfg.ClaudeKey) {
		changes = append(changes, fmt.Sprintf("claude-api-key count: %d -> %d", len(oldCfg.ClaudeKey), len(newCfg.ClaudeKey)))
	} else {
		for i := range oldCfg.ClaudeKey {
			if i >= len(newCfg.ClaudeKey) {
				break
			}
			o := oldCfg.ClaudeKey[i]
			n := newCfg.ClaudeKey[i]
			if strings.TrimSpace(o.BaseURL) != strings.TrimSpace(n.BaseURL) {
				changes = append(changes, fmt.Sprintf("claude[%d].base-url: %s -> %s", i, strings.TrimSpace(o.BaseURL), strings.TrimSpace(n.BaseURL)))
			}
			if strings.TrimSpace(o.ProxyURL) != strings.TrimSpace(n.ProxyURL) {
				changes = append(changes, fmt.Sprintf("claude[%d].proxy-url: %s -> %s", i, strings.TrimSpace(o.ProxyURL), strings.TrimSpace(n.ProxyURL)))
			}
			if strings.TrimSpace(o.APIKey) != strings.TrimSpace(n.APIKey) {
				changes = append(changes, fmt.Sprintf("claude[%d].api-key: updated", i))
			}
			if !equalStringMap(o.Headers, n.Headers) {
				changes = append(changes, fmt.Sprintf("claude[%d].headers: updated", i))
			}
			oldExcluded := summarizeExcludedModels(o.ExcludedModels)
			newExcluded := summarizeExcludedModels(n.ExcludedModels)
			if oldExcluded.hash != newExcluded.hash {
				changes = append(changes, fmt.Sprintf("claude[%d].excluded-models: updated (%d -> %d entries)", i, oldExcluded.count, newExcluded.count))
			}
		}
	}

	// Codex keys (do not print key material)
	if len(oldCfg.CodexKey) != len(newCfg.CodexKey) {
		changes = append(changes, fmt.Sprintf("codex-api-key count: %d -> %d", len(oldCfg.CodexKey), len(newCfg.CodexKey)))
	} else {
		for i := range oldCfg.CodexKey {
			if i >= len(newCfg.CodexKey) {
				break
			}
			o := oldCfg.CodexKey[i]
			n := newCfg.CodexKey[i]
			if strings.TrimSpace(o.BaseURL) != strings.TrimSpace(n.BaseURL) {
				changes = append(changes, fmt.Sprintf("codex[%d].base-url: %s -> %s", i, strings.TrimSpace(o.BaseURL), strings.TrimSpace(n.BaseURL)))
			}
			if strings.TrimSpace(o.ProxyURL) != strings.TrimSpace(n.ProxyURL) {
				changes = append(changes, fmt.Sprintf("codex[%d].proxy-url: %s -> %s", i, strings.TrimSpace(o.ProxyURL), strings.TrimSpace(n.ProxyURL)))
			}
			if strings.TrimSpace(o.APIKey) != strings.TrimSpace(n.APIKey) {
				changes = append(changes, fmt.Sprintf("codex[%d].api-key: updated", i))
			}
			if !equalStringMap(o.Headers, n.Headers) {
				changes = append(changes, fmt.Sprintf("codex[%d].headers: updated", i))
			}
			oldExcluded := summarizeExcludedModels(o.ExcludedModels)
			newExcluded := summarizeExcludedModels(n.ExcludedModels)
			if oldExcluded.hash != newExcluded.hash {
				changes = append(changes, fmt.Sprintf("codex[%d].excluded-models: updated (%d -> %d entries)", i, oldExcluded.count, newExcluded.count))
			}
		}
	}

	// AmpCode settings (redacted where needed)
	oldAmpURL := strings.TrimSpace(oldCfg.AmpCode.UpstreamURL)
	newAmpURL := strings.TrimSpace(newCfg.AmpCode.UpstreamURL)
	if oldAmpURL != newAmpURL {
		changes = append(changes, fmt.Sprintf("ampcode.upstream-url: %s -> %s", oldAmpURL, newAmpURL))
	}
	oldAmpKey := strings.TrimSpace(oldCfg.AmpCode.UpstreamAPIKey)
	newAmpKey := strings.TrimSpace(newCfg.AmpCode.UpstreamAPIKey)
	switch {
	case oldAmpKey == "" && newAmpKey != "":
		changes = append(changes, "ampcode.upstream-api-key: added")
	case oldAmpKey != "" && newAmpKey == "":
		changes = append(changes, "ampcode.upstream-api-key: removed")
	case oldAmpKey != newAmpKey:
		changes = append(changes, "ampcode.upstream-api-key: updated")
	}
	if oldCfg.AmpCode.RestrictManagementToLocalhost != newCfg.AmpCode.RestrictManagementToLocalhost {
		changes = append(changes, fmt.Sprintf("ampcode.restrict-management-to-localhost: %t -> %t", oldCfg.AmpCode.RestrictManagementToLocalhost, newCfg.AmpCode.RestrictManagementToLocalhost))
	}
	oldMappings := summarizeAmpModelMappings(oldCfg.AmpCode.ModelMappings)
	newMappings := summarizeAmpModelMappings(newCfg.AmpCode.ModelMappings)
	if oldMappings.hash != newMappings.hash {
		changes = append(changes, fmt.Sprintf("ampcode.model-mappings: updated (%d -> %d entries)", oldMappings.count, newMappings.count))
	}

	if entries, _ := diffOAuthExcludedModelChanges(oldCfg.OAuthExcludedModels, newCfg.OAuthExcludedModels); len(entries) > 0 {
		changes = append(changes, entries...)
	}

	// Remote management (never print the key)
	if oldCfg.RemoteManagement.AllowRemote != newCfg.RemoteManagement.AllowRemote {
		changes = append(changes, fmt.Sprintf("remote-management.allow-remote: %t -> %t", oldCfg.RemoteManagement.AllowRemote, newCfg.RemoteManagement.AllowRemote))
	}
	if oldCfg.RemoteManagement.DisableControlPanel != newCfg.RemoteManagement.DisableControlPanel {
		changes = append(changes, fmt.Sprintf("remote-management.disable-control-panel: %t -> %t", oldCfg.RemoteManagement.DisableControlPanel, newCfg.RemoteManagement.DisableControlPanel))
	}
	if oldCfg.RemoteManagement.SecretKey != newCfg.RemoteManagement.SecretKey {
		switch {
		case oldCfg.RemoteManagement.SecretKey == "" && newCfg.RemoteManagement.SecretKey != "":
			changes = append(changes, "remote-management.secret-key: created")
		case oldCfg.RemoteManagement.SecretKey != "" && newCfg.RemoteManagement.SecretKey == "":
			changes = append(changes, "remote-management.secret-key: deleted")
		default:
			changes = append(changes, "remote-management.secret-key: updated")
		}
	}

	// OpenAI compatibility providers (summarized)
	if compat := diffOpenAICompatibility(oldCfg.OpenAICompatibility, newCfg.OpenAICompatibility); len(compat) > 0 {
		changes = append(changes, "openai-compatibility:")
		for _, c := range compat {
			changes = append(changes, "  "+c)
		}
	}

	return changes
}

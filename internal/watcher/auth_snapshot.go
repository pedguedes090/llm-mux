package watcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nghyane/llm-mux/internal/runtime/geminicli"
	coreauth "github.com/nghyane/llm-mux/sdk/cliproxy/auth"
)

// SnapshotCoreAuths converts current clients snapshot into core auth entries.
func (w *Watcher) SnapshotCoreAuths() []*coreauth.Auth {
	out := make([]*coreauth.Auth, 0, 32)
	now := time.Now()
	idGen := newStableIDGenerator()
	// Also synthesize auth entries for OpenAI-compatibility providers directly from config
	w.clientsMutex.RLock()
	cfg := w.config
	w.clientsMutex.RUnlock()
	if cfg != nil {
		// Gemini official API keys -> synthesize auths
		for i := range cfg.GeminiKey {
			entry := cfg.GeminiKey[i]
			key := strings.TrimSpace(entry.APIKey)
			if key == "" {
				continue
			}
			base := strings.TrimSpace(entry.BaseURL)
			proxyURL := strings.TrimSpace(entry.ProxyURL)
			id, token := idGen.next("gemini:apikey", key, base)
			attrs := map[string]string{
				"source":  fmt.Sprintf("config:gemini[%s]", token),
				"api_key": key,
			}
			if base != "" {
				attrs["base_url"] = base
			}
			addConfigHeadersToAttrs(entry.Headers, attrs)
			a := &coreauth.Auth{
				ID:         id,
				Provider:   "gemini",
				Label:      "gemini-apikey",
				Status:     coreauth.StatusActive,
				ProxyURL:   proxyURL,
				Attributes: attrs,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			applyAuthExcludedModelsMeta(a, cfg, entry.ExcludedModels, "apikey")
			out = append(out, a)
		}

		// Claude API keys -> synthesize auths
		for i := range cfg.ClaudeKey {
			ck := cfg.ClaudeKey[i]
			key := strings.TrimSpace(ck.APIKey)
			if key == "" {
				continue
			}
			base := strings.TrimSpace(ck.BaseURL)
			id, token := idGen.next("claude:apikey", key, base)
			attrs := map[string]string{
				"source":  fmt.Sprintf("config:claude[%s]", token),
				"api_key": key,
			}
			if base != "" {
				attrs["base_url"] = base
			}
			if hash := computeClaudeModelsHash(ck.Models); hash != "" {
				attrs["models_hash"] = hash
			}
			addConfigHeadersToAttrs(ck.Headers, attrs)
			proxyURL := strings.TrimSpace(ck.ProxyURL)
			a := &coreauth.Auth{
				ID:         id,
				Provider:   "claude",
				Label:      "claude-apikey",
				Status:     coreauth.StatusActive,
				ProxyURL:   proxyURL,
				Attributes: attrs,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			applyAuthExcludedModelsMeta(a, cfg, ck.ExcludedModels, "apikey")
			out = append(out, a)
		}
		// Codex API keys -> synthesize auths
		for i := range cfg.CodexKey {
			ck := cfg.CodexKey[i]
			key := strings.TrimSpace(ck.APIKey)
			if key == "" {
				continue
			}
			id, token := idGen.next("codex:apikey", key, ck.BaseURL)
			attrs := map[string]string{
				"source":  fmt.Sprintf("config:codex[%s]", token),
				"api_key": key,
			}
			if ck.BaseURL != "" {
				attrs["base_url"] = ck.BaseURL
			}
			addConfigHeadersToAttrs(ck.Headers, attrs)
			proxyURL := strings.TrimSpace(ck.ProxyURL)
			a := &coreauth.Auth{
				ID:         id,
				Provider:   "codex",
				Label:      "codex-apikey",
				Status:     coreauth.StatusActive,
				ProxyURL:   proxyURL,
				Attributes: attrs,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			applyAuthExcludedModelsMeta(a, cfg, ck.ExcludedModels, "apikey")
			out = append(out, a)
		}
		for i := range cfg.OpenAICompatibility {
			compat := &cfg.OpenAICompatibility[i]
			providerName := strings.ToLower(strings.TrimSpace(compat.Name))
			if providerName == "" {
				providerName = "openai-compatibility"
			}
			base := strings.TrimSpace(compat.BaseURL)

			// Handle new APIKeyEntries format (preferred)
			createdEntries := 0
			for j := range compat.APIKeyEntries {
				entry := &compat.APIKeyEntries[j]
				key := strings.TrimSpace(entry.APIKey)
				proxyURL := strings.TrimSpace(entry.ProxyURL)
				idKind := fmt.Sprintf("openai-compatibility:%s", providerName)
				id, token := idGen.next(idKind, key, base, proxyURL)
				attrs := map[string]string{
					"source":       fmt.Sprintf("config:%s[%s]", providerName, token),
					"base_url":     base,
					"compat_name":  compat.Name,
					"provider_key": providerName,
				}
				if key != "" {
					attrs["api_key"] = key
				}
				if hash := computeOpenAICompatModelsHash(compat.Models); hash != "" {
					attrs["models_hash"] = hash
				}
				addConfigHeadersToAttrs(compat.Headers, attrs)
				a := &coreauth.Auth{
					ID:         id,
					Provider:   providerName,
					Label:      compat.Name,
					Status:     coreauth.StatusActive,
					ProxyURL:   proxyURL,
					Attributes: attrs,
					CreatedAt:  now,
					UpdatedAt:  now,
				}
				out = append(out, a)
				createdEntries++
			}
			if createdEntries == 0 {
				idKind := fmt.Sprintf("openai-compatibility:%s", providerName)
				id, token := idGen.next(idKind, base)
				attrs := map[string]string{
					"source":       fmt.Sprintf("config:%s[%s]", providerName, token),
					"base_url":     base,
					"compat_name":  compat.Name,
					"provider_key": providerName,
				}
				if hash := computeOpenAICompatModelsHash(compat.Models); hash != "" {
					attrs["models_hash"] = hash
				}
				addConfigHeadersToAttrs(compat.Headers, attrs)
				a := &coreauth.Auth{
					ID:         id,
					Provider:   providerName,
					Label:      compat.Name,
					Status:     coreauth.StatusActive,
					Attributes: attrs,
					CreatedAt:  now,
					UpdatedAt:  now,
				}
				out = append(out, a)
			}
		}
	}

	// Process Vertex API key providers (Vertex-compatible endpoints)
	for i := range cfg.VertexCompatAPIKey {
		compat := &cfg.VertexCompatAPIKey[i]
		providerName := "vertex"
		base := strings.TrimSpace(compat.BaseURL)

		key := strings.TrimSpace(compat.APIKey)
		proxyURL := strings.TrimSpace(compat.ProxyURL)
		idKind := fmt.Sprintf("vertex:apikey:%s", base)
		id, token := idGen.next(idKind, key, base, proxyURL)
		attrs := map[string]string{
			"source":       fmt.Sprintf("config:vertex-apikey[%s]", token),
			"base_url":     base,
			"provider_key": providerName,
		}
		if key != "" {
			attrs["api_key"] = key
		}
		if hash := computeVertexCompatModelsHash(compat.Models); hash != "" {
			attrs["models_hash"] = hash
		}
		addConfigHeadersToAttrs(compat.Headers, attrs)
		a := &coreauth.Auth{
			ID:         id,
			Provider:   providerName,
			Label:      "vertex-apikey",
			Status:     coreauth.StatusActive,
			ProxyURL:   proxyURL,
			Attributes: attrs,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		applyAuthExcludedModelsMeta(a, cfg, nil, "apikey")
		out = append(out, a)
	}

	// Also synthesize auth entries directly from auth files (for OAuth/file-backed providers)
	entries, _ := os.ReadDir(w.authDir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		full := filepath.Join(w.authDir, name)
		data, err := os.ReadFile(full)
		if err != nil || len(data) == 0 {
			continue
		}
		var metadata map[string]any
		if err = json.Unmarshal(data, &metadata); err != nil {
			continue
		}
		t, _ := metadata["type"].(string)
		if t == "" {
			continue
		}
		provider := strings.ToLower(t)
		if provider == "gemini" {
			provider = "gemini-cli"
		}
		label := provider
		if email, _ := metadata["email"].(string); email != "" {
			label = email
		}
		// Use relative path under authDir as ID to stay consistent with the file-based token store
		id := full
		if rel, errRel := filepath.Rel(w.authDir, full); errRel == nil && rel != "" {
			id = rel
		}

		proxyURL := ""
		if p, ok := metadata["proxy_url"].(string); ok {
			proxyURL = p
		}

		a := &coreauth.Auth{
			ID:       id,
			Provider: provider,
			Label:    label,
			Status:   coreauth.StatusActive,
			Attributes: map[string]string{
				"source": full,
				"path":   full,
			},
			ProxyURL:  proxyURL,
			Metadata:  metadata,
			CreatedAt: now,
			UpdatedAt: now,
		}
		applyAuthExcludedModelsMeta(a, cfg, nil, "oauth")
		if provider == "gemini-cli" {
			if virtuals := synthesizeGeminiVirtualAuths(a, metadata, now); len(virtuals) > 0 {
				for _, v := range virtuals {
					applyAuthExcludedModelsMeta(v, cfg, nil, "oauth")
				}
				out = append(out, a)
				out = append(out, virtuals...)
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

func synthesizeGeminiVirtualAuths(primary *coreauth.Auth, metadata map[string]any, now time.Time) []*coreauth.Auth {
	if primary == nil || metadata == nil {
		return nil
	}
	projects := splitGeminiProjectIDs(metadata)
	if len(projects) <= 1 {
		return nil
	}
	email, _ := metadata["email"].(string)
	shared := geminicli.NewSharedCredential(primary.ID, email, metadata, projects)
	primary.Disabled = true
	primary.Status = coreauth.StatusDisabled
	primary.Runtime = shared
	if primary.Attributes == nil {
		primary.Attributes = make(map[string]string)
	}
	primary.Attributes["gemini_virtual_primary"] = "true"
	primary.Attributes["virtual_children"] = strings.Join(projects, ",")
	source := primary.Attributes["source"]
	authPath := primary.Attributes["path"]
	originalProvider := primary.Provider
	if originalProvider == "" {
		originalProvider = "gemini-cli"
	}
	label := primary.Label
	if label == "" {
		label = originalProvider
	}
	virtuals := make([]*coreauth.Auth, 0, len(projects))
	for _, projectID := range projects {
		attrs := map[string]string{
			"runtime_only":           "true",
			"gemini_virtual_parent":  primary.ID,
			"gemini_virtual_project": projectID,
		}
		if source != "" {
			attrs["source"] = source
		}
		if authPath != "" {
			attrs["path"] = authPath
		}
		metadataCopy := map[string]any{
			"email":             email,
			"project_id":        projectID,
			"virtual":           true,
			"virtual_parent_id": primary.ID,
			"type":              metadata["type"],
		}
		proxy := strings.TrimSpace(primary.ProxyURL)
		if proxy != "" {
			metadataCopy["proxy_url"] = proxy
		}
		virtual := &coreauth.Auth{
			ID:         buildGeminiVirtualID(primary.ID, projectID),
			Provider:   originalProvider,
			Label:      fmt.Sprintf("%s [%s]", label, projectID),
			Status:     coreauth.StatusActive,
			Attributes: attrs,
			Metadata:   metadataCopy,
			ProxyURL:   primary.ProxyURL,
			CreatedAt:  now,
			UpdatedAt:  now,
			Runtime:    geminicli.NewVirtualCredential(projectID, shared),
		}
		virtuals = append(virtuals, virtual)
	}
	return virtuals
}

func splitGeminiProjectIDs(metadata map[string]any) []string {
	raw, _ := metadata["project_id"].(string)
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func buildGeminiVirtualID(baseID, projectID string) string {
	project := strings.TrimSpace(projectID)
	if project == "" {
		project = "project"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_")
	return fmt.Sprintf("%s::%s", baseID, replacer.Replace(project))
}

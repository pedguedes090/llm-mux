package provider

import (
	"context"
	"math/rand"
	"time"
)

type AntigravityStrategy struct{}

func (s *AntigravityStrategy) Score(auth *Auth, state *AuthQuotaState, config *ProviderQuotaConfig) int64 {
	if state == nil {
		return 0
	}

	var priority int64
	priority += state.ActiveRequests.Load() * 1000

	if real := state.GetRealQuota(); real != nil && time.Since(real.FetchedAt) < 5*time.Minute {
		priority += int64((1.0 - real.RemainingFraction) * 800)
	} else {
		limit := state.LearnedLimit.Load()
		if limit <= 0 && config != nil {
			limit = config.EstimatedLimit
		}
		if limit > 0 {
			used := state.TotalTokensUsed.Load()
			priority += int64(float64(used) / float64(limit) * 500)
		}
	}

	tokenValid, needsRefresh := checkTokenFromAuth(auth, 30*time.Second)
	if !tokenValid {
		priority += 10000
	} else if needsRefresh {
		priority += 500
	}

	return priority
}

func (s *AntigravityStrategy) OnQuotaHit(state *AuthQuotaState, cooldown *time.Duration) {
	if state == nil {
		return
	}
	now := time.Now()
	state.SetLastExhaustedAt(now)

	tokensUsed := state.TotalTokensUsed.Load()
	for {
		currentLimit := state.LearnedLimit.Load()
		if tokensUsed <= currentLimit {
			break
		}
		if state.LearnedLimit.CompareAndSwap(currentLimit, tokensUsed) {
			break
		}
	}

	if cooldown != nil && *cooldown > 0 {
		state.SetCooldownUntil(now.Add(*cooldown))
	} else {
		state.SetCooldownUntil(now.Add(5 * time.Hour))
	}

	state.TotalTokensUsed.Store(0)
}

func (s *AntigravityStrategy) RecordUsage(state *AuthQuotaState, tokens int64) {
	if state != nil && tokens > 0 {
		state.TotalTokensUsed.Add(tokens)
	}
}

func (s *AntigravityStrategy) StartRefresh(ctx context.Context, auth *Auth) <-chan *RealQuotaSnapshot {
	ch := make(chan *RealQuotaSnapshot, 1)

	if auth == nil {
		close(ch)
		return ch
	}

	go func() {
		defer close(ch)

		jitter := time.Duration(rand.Float64() * float64(30*time.Second))
		time.Sleep(jitter)

		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()

		fetchQuota := func() {
			token := extractAccessToken(auth)
			if token == "" {
				return
			}
			if snapshot := fetchAntigravityQuota(ctx, token); snapshot != nil {
				select {
				case ch <- snapshot:
				default:
				}
			}
		}

		fetchQuota()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fetchQuota()
			}
		}
	}()
	return ch
}

func extractAccessToken(auth *Auth) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	if v, ok := auth.Metadata["access_token"].(string); ok {
		return v
	}
	return ""
}

func checkTokenFromAuth(auth *Auth, buffer time.Duration) (valid bool, needsRefresh bool) {
	if auth == nil || auth.Metadata == nil {
		return false, false
	}

	token := extractAccessToken(auth)
	if token == "" {
		return false, false
	}

	var expiresAt time.Time
	if expiredStr, ok := auth.Metadata["expired"].(string); ok && expiredStr != "" {
		if t, err := time.Parse(time.RFC3339, expiredStr); err == nil {
			expiresAt = t
		}
	}

	if expiresAt.IsZero() {
		if ts, ok := auth.Metadata["timestamp"].(int64); ok {
			if expiresIn, ok := auth.Metadata["expires_in"].(int64); ok && expiresIn > 0 {
				expiresAt = time.UnixMilli(ts).Add(time.Duration(expiresIn) * time.Second)
			}
		}
	}

	if expiresAt.IsZero() {
		return true, false
	}

	now := time.Now()
	valid = now.Add(buffer).Before(expiresAt)
	needsRefresh = expiresAt.Sub(now) < 5*time.Minute
	return valid, needsRefresh
}

var (
	_ ProviderStrategy    = (*AntigravityStrategy)(nil)
	_ BackgroundRefresher = (*AntigravityStrategy)(nil)
)

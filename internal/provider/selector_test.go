package provider

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestStickyStoreSharding(t *testing.T) {
	store := NewStickyStore()
	store.Start()
	defer store.Stop()

	for i := 0; i < 100; i++ {
		key := "provider:" + string(rune('a'+i/10)) + string(rune('0'+i%10))
		store.Set(key, "auth"+string(rune('0'+i%10)))
	}

	if store.Len() != 100 {
		t.Errorf("Expected 100 entries, got %d", store.Len())
	}
}

func TestStickyStoreEviction(t *testing.T) {
	store := NewStickyStore()
	store.Start()
	defer store.Stop()

	shard := store.shards[0]

	for i := 0; i < maxEntriesPerShard+10; i++ {
		shard.mu.Lock()
		key := "key" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		shard.entries[key] = &stickyEntry{
			authID:   "auth",
			lastUsed: time.Now(),
		}
		if len(shard.entries) >= maxEntriesPerShard {
			store.evictOldest(shard, time.Now())
		}
		shard.mu.Unlock()
	}

	shard.mu.RLock()
	count := len(shard.entries)
	shard.mu.RUnlock()

	if count > maxEntriesPerShard {
		t.Errorf("Expected <= %d entries, got %d", maxEntriesPerShard, count)
	}
}

func TestConcurrentPick(t *testing.T) {
	selector := &RoundRobinSelector{}
	selector.Start()
	defer selector.Stop()

	auths := []*Auth{
		{ID: "auth1", Provider: "gemini"},
		{ID: "auth2", Provider: "gemini"},
		{ID: "auth3", Provider: "gemini"},
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, err := selector.Pick(context.Background(), "gemini", "model", Options{}, auths)
				if err != nil {
					t.Errorf("Pick failed: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestForceRotate(t *testing.T) {
	selector := &RoundRobinSelector{}
	selector.Start()
	defer selector.Stop()

	auths := []*Auth{
		{ID: "auth1", Provider: "gemini"},
		{ID: "auth2", Provider: "gemini"},
	}

	first, _ := selector.Pick(context.Background(), "gemini", "model", Options{}, auths)

	sticky, _ := selector.Pick(context.Background(), "gemini", "model", Options{}, auths)
	if sticky.ID != first.ID {
		t.Errorf("Expected sticky session to return same auth, got %s vs %s", sticky.ID, first.ID)
	}

	rotated, _ := selector.Pick(context.Background(), "gemini", "model", Options{ForceRotate: true}, auths)
	if rotated.ID == first.ID {
		t.Error("Expected ForceRotate to select different auth")
	}
}

func TestGracefulShutdown(t *testing.T) {
	selector := &RoundRobinSelector{}
	selector.Start()

	auths := []*Auth{{ID: "auth1", Provider: "gemini"}}
	_, err := selector.Pick(context.Background(), "gemini", "model", Options{}, auths)
	if err != nil {
		t.Fatalf("Pick failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		selector.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not complete within timeout")
	}
}

func BenchmarkPick(b *testing.B) {
	selector := &RoundRobinSelector{}
	selector.Start()
	defer selector.Stop()

	auths := []*Auth{
		{ID: "auth1", Provider: "gemini"},
		{ID: "auth2", Provider: "gemini"},
		{ID: "auth3", Provider: "gemini"},
	}

	ctx := context.Background()
	opts := Options{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = selector.Pick(ctx, "gemini", "model", opts, auths)
	}
}

func BenchmarkPickParallel(b *testing.B) {
	selector := &RoundRobinSelector{}
	selector.Start()
	defer selector.Stop()

	auths := []*Auth{
		{ID: "auth1", Provider: "gemini"},
		{ID: "auth2", Provider: "gemini"},
		{ID: "auth3", Provider: "gemini"},
	}

	ctx := context.Background()
	opts := Options{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = selector.Pick(ctx, "gemini", "model", opts, auths)
		}
	})
}

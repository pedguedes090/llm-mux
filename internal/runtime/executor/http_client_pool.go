package executor

import (
	"net/http"
	"sync"
)

// httpClientPool pools http.Client instances keyed by transport.
// Since http.Client is a lightweight struct (~3 pointers), the real benefit
// is ensuring connection reuse via shared transports.
var httpClientPool = sync.Pool{
	New: func() any {
		return &http.Client{}
	},
}

// transportCache caches proxy transports by URL to enable connection pooling
// for proxied requests. Without this, each request creates a new transport
// which breaks HTTP/2 connection reuse.
var (
	transportCache   = make(map[string]*http.Transport)
	transportCacheMu sync.RWMutex
)

// AcquireHTTPClient gets an http.Client from the pool.
// The returned client has no timeout set - callers should use context
// for request timeouts instead of http.Client.Timeout.
func AcquireHTTPClient() *http.Client {
	return httpClientPool.Get().(*http.Client)
}

// ReleaseHTTPClient returns an http.Client to the pool after resetting its state.
// The transport is preserved since it manages the connection pool.
func ReleaseHTTPClient(c *http.Client) {
	if c == nil {
		return
	}
	// Reset client state but preserve transport for connection reuse
	c.Timeout = 0
	c.CheckRedirect = nil
	c.Jar = nil
	httpClientPool.Put(c)
}

// getCachedTransport returns a cached transport for the given proxy URL,
// creating one if it doesn't exist. Returns nil for empty proxyURL.
func getCachedTransport(proxyURL string) *http.Transport {
	if proxyURL == "" {
		return nil
	}

	// Fast path: read lock
	transportCacheMu.RLock()
	if t, ok := transportCache[proxyURL]; ok {
		transportCacheMu.RUnlock()
		return t
	}
	transportCacheMu.RUnlock()

	// Slow path: write lock
	transportCacheMu.Lock()
	defer transportCacheMu.Unlock()

	// Double-check after acquiring write lock
	if t, ok := transportCache[proxyURL]; ok {
		return t
	}

	// Build and cache the transport
	t := buildProxyTransport(proxyURL)
	if t != nil {
		transportCache[proxyURL] = t
	}
	return t
}

// ClearTransportCache clears all cached proxy transports.
// Useful for testing or when proxy configuration changes.
func ClearTransportCache() {
	transportCacheMu.Lock()
	defer transportCacheMu.Unlock()

	// Close idle connections on each transport before clearing
	for _, t := range transportCache {
		t.CloseIdleConnections()
	}
	transportCache = make(map[string]*http.Transport)
}

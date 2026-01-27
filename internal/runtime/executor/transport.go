package executor

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"

	"github.com/nghyane/llm-mux/internal/transport"
	"golang.org/x/net/http2"
)

func configureHTTP2(t *http.Transport) {
	h2Transport, err := http2.ConfigureTransports(t)
	if err != nil {
		return
	}
	h2Transport.ReadIdleTimeout = transport.Config.H2ReadIdleTimeout
	h2Transport.PingTimeout = transport.Config.H2PingTimeout
	h2Transport.StrictMaxConcurrentStreams = transport.Config.H2StrictMaxConcurrentStreams
	h2Transport.AllowHTTP = transport.Config.H2AllowHTTP
}

func newDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   transport.Config.DialTimeout,
		KeepAlive: transport.Config.KeepAlive,
		DualStack: true,
	}
}

func baseTransport() *http.Transport {
	t := &http.Transport{
		MaxIdleConns:        transport.Config.MaxIdleConns,
		MaxIdleConnsPerHost: transport.Config.MaxIdleConnsPerHost,
		MaxConnsPerHost:     transport.Config.MaxConnsPerHost,
		IdleConnTimeout:     transport.Config.IdleConnTimeout,

		TLSHandshakeTimeout:   transport.Config.TLSHandshakeTimeout,
		ExpectContinueTimeout: transport.Config.ExpectContinueTimeout,
		ResponseHeaderTimeout: transport.Config.ResponseHeaderTimeout,

		ForceAttemptHTTP2:  true,
		DisableCompression: false,

		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},

		WriteBufferSize: 256 * 1024, // 256KB - increased from 64KB for better streaming throughput
		ReadBufferSize:  256 * 1024, // 256KB - increased from 64KB for better streaming throughput
	}
	configureHTTP2(t)
	return t
}

var SharedTransport = baseTransport()

func init() {
	SharedTransport.DialContext = newDialer().DialContext
}

func ProxyTransport(proxyURL *url.URL) *http.Transport {
	t := baseTransport()
	t.Proxy = http.ProxyURL(proxyURL)
	t.DialContext = newDialer().DialContext
	return t
}

func SOCKS5Transport(dialFunc func(network, addr string) (net.Conn, error)) *http.Transport {
	t := baseTransport()
	t.DialContext = func(_ context.Context, network, addr string) (net.Conn, error) {
		return dialFunc(network, addr)
	}
	return t
}

func CloseIdleConnections() {
	SharedTransport.CloseIdleConnections()
}

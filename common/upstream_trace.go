package common

import (
	"context"
	"net/http/httptrace"
	"net/url"
	"sync"
	"time"
)

type upstreamRequestTraceContextKey struct{}

type UpstreamRequestTrace struct {
	mu                   sync.Mutex
	dnsAddrs             []string
	dnsCoalesced         bool
	connectNetwork       string
	connectAddr          string
	remoteAddr           string
	reusedConnection     bool
	wasIdle              bool
	idleTimeMilliseconds int64
	proxyConfigured      bool
	proxyScheme          string
	wroteRequestError    string
	gotFirstByte         bool
}

func NewUpstreamRequestTrace() *UpstreamRequestTrace {
	return &UpstreamRequestTrace{}
}

func ContextWithUpstreamRequestTrace(ctx context.Context, trace *UpstreamRequestTrace) context.Context {
	if ctx == nil || trace == nil {
		return ctx
	}
	return context.WithValue(ctx, upstreamRequestTraceContextKey{}, trace)
}

func UpstreamRequestTraceFromContext(ctx context.Context) *UpstreamRequestTrace {
	if ctx == nil {
		return nil
	}
	trace, _ := ctx.Value(upstreamRequestTraceContextKey{}).(*UpstreamRequestTrace)
	return trace
}

func (t *UpstreamRequestTrace) SetProxyURL(proxyURL string) {
	if t == nil || proxyURL == "" {
		return
	}
	scheme := ""
	if parsedURL, err := url.Parse(proxyURL); err == nil {
		scheme = parsedURL.Scheme
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.proxyConfigured = true
	t.proxyScheme = scheme
}

func (t *UpstreamRequestTrace) ClientTrace() *httptrace.ClientTrace {
	if t == nil {
		return nil
	}
	return &httptrace.ClientTrace{
		DNSDone: func(info httptrace.DNSDoneInfo) {
			addrs := make([]string, 0, len(info.Addrs))
			for _, addr := range info.Addrs {
				addrs = append(addrs, addr.String())
			}
			t.mu.Lock()
			t.dnsAddrs = addrs
			t.dnsCoalesced = info.Coalesced
			t.mu.Unlock()
		},
		ConnectStart: func(network, addr string) {
			t.mu.Lock()
			t.connectNetwork = network
			t.connectAddr = addr
			t.mu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			remoteAddr := ""
			if info.Conn != nil && info.Conn.RemoteAddr() != nil {
				remoteAddr = info.Conn.RemoteAddr().String()
			}

			t.mu.Lock()
			t.remoteAddr = remoteAddr
			t.reusedConnection = info.Reused
			t.wasIdle = info.WasIdle
			t.idleTimeMilliseconds = info.IdleTime.Round(time.Millisecond).Milliseconds()
			t.mu.Unlock()
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				return
			}
			t.mu.Lock()
			t.wroteRequestError = LocalLogPreview(info.Err.Error())
			t.mu.Unlock()
		},
		GotFirstResponseByte: func() {
			t.mu.Lock()
			t.gotFirstByte = true
			t.mu.Unlock()
		},
	}
}

func (t *UpstreamRequestTrace) Snapshot() map[string]interface{} {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	result := make(map[string]interface{})
	if len(t.dnsAddrs) > 0 {
		result["dns_addrs"] = append([]string(nil), t.dnsAddrs...)
		result["dns_coalesced"] = t.dnsCoalesced
	}
	if t.connectNetwork != "" {
		result["connect_network"] = t.connectNetwork
	}
	if t.connectAddr != "" {
		result["connect_addr"] = t.connectAddr
	}
	if t.remoteAddr != "" {
		result["remote_addr"] = t.remoteAddr
	}
	result["reused_connection"] = t.reusedConnection
	result["was_idle"] = t.wasIdle
	if t.idleTimeMilliseconds > 0 {
		result["idle_time_ms"] = t.idleTimeMilliseconds
	}
	if t.proxyConfigured {
		result["proxy_configured"] = true
		if t.proxyScheme != "" {
			result["proxy_scheme"] = t.proxyScheme
		}
	}
	if t.wroteRequestError != "" {
		result["wrote_request_error"] = t.wroteRequestError
	}
	result["got_first_response_byte"] = t.gotFirstByte
	return result
}

// Package proxy implements a reverse proxy that forwards MCP requests to an upstream server.
package proxy

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

const maxRequestBody = 1 << 20 // 1 MB

type Handler struct {
	upstream *url.URL
	proxy    *httputil.ReverseProxy
}

func NewHandler(upstreamURL string) (*Handler, error) {
	u, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, err
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("upstream URL scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("upstream URL must have a host")
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			req.URL.Path = u.Path
			req.Host = u.Host
		},
		Transport: transport,
		ModifyResponse: func(resp *http.Response) error {
			slog.Info("upstream response",
				"status", resp.StatusCode,
				"session_id", resp.Header.Get("Mcp-Session-Id"),
			)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("proxy error", "error", err)
			http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32603,"message":"upstream unavailable"}}`, http.StatusBadGateway)
		},
	}

	return &Handler{upstream: u, proxy: rp}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	h.proxy.ServeHTTP(w, r)
}

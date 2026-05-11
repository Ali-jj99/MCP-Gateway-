// Package proxy implements a reverse proxy that forwards MCP requests to an upstream server.
package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
)

type Handler struct {
	upstream *url.URL
	proxy    *httputil.ReverseProxy
}

func NewHandler(upstreamURL string) (*Handler, error) {
	u, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, err
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			req.URL.Path = u.Path
			req.Host = u.Host
		},
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
	w.Header().Set("Content-Type", "application/json")
	h.proxy.ServeHTTP(w, r)
}

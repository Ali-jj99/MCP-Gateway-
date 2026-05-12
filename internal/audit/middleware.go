package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
)

const maxBodyCapture = 64 * 1024

type responseCapture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.status = code
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	if rc.body.Len() < maxBodyCapture {
		remaining := maxBodyCapture - rc.body.Len()
		if len(b) > remaining {
			rc.body.Write(b[:remaining])
		} else {
			rc.body.Write(b)
		}
	}
	return rc.ResponseWriter.Write(b)
}

func (l *Logger) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		reqBody := readAndRestore(r)

		rc := &responseCapture{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rc, r)

		apiKeyID := uuid.Nil
		if key, ok := auth.APIKeyFromContext(r.Context()); ok {
			apiKeyID = key.ID
		}

		l.Log(Entry{
			ApiKeyID:     apiKeyID,
			Action:       r.Method,
			Resource:     r.URL.Path,
			StatusCode:   rc.status,
			LatencyMs:    time.Since(start).Milliseconds(),
			IP:           clientIP(r),
			RequestBody:  reqBody,
			ResponseBody: rc.body.String(),
			ToolName:     extractToolName(reqBody),
		})
	})
}

func readAndRestore(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyCapture))
	r.Body.Close()
	if err != nil {
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return string(body)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, ok := strings.Cut(r.RemoteAddr, ":"); ok {
		return host
	}
	return r.RemoteAddr
}

func extractToolName(body string) string {
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal([]byte(body), &req) != nil {
		return ""
	}
	if req.Method == "tools/call" && req.Params.Name != "" {
		return req.Params.Name
	}
	return req.Method
}

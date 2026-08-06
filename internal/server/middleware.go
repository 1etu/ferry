package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
)

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; connect-src 'self'; font-src 'self'; worker-src 'self'; object-src 'none'; base-uri 'none'; " +
	"frame-ancestors 'none'; form-action 'self'"

func (s *server) chain(mux http.Handler) http.Handler {
	h := recordRequest(mux)
	h = s.Auth.CheckOrigin(h)
	h = s.Auth.Identify(h)
	h = s.Auth.CheckHost(h)
	h = securityHeaders(h)
	h = s.logRequests(h)
	return s.recoverPanics(h)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 && status >= http.StatusOK {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) ReadFrom(src io.Reader) (int64, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return io.Copy(w.ResponseWriter, src)
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusWriter) isWritten() bool {
	return w.status != 0
}

func (w *statusWriter) sentStatus() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (s *server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if err, isError := recovered.(error); isError && errors.Is(err, http.ErrAbortHandler) {
				panic(recovered)
			}
			s.Log.Error("request panicked", "method", r.Method, "path", r.URL.Path,
				"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
			if !sw.isWritten() {
				s.writeError(sw, http.StatusInternalServerError, api.CodeInternal, "internal error")
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

type requestInfo struct {
	route  string
	remote string
	device string
}

type requestInfoKey struct{}

func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.Log.Enabled(r.Context(), slog.LevelDebug) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		info := &requestInfo{remote: r.RemoteAddr}
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info)))
		s.Log.Debug("request",
			"method", r.Method,
			"route", info.route,
			"status", sw.sentStatus(),
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", info.remote,
			"device", info.device,
		)
	})
}

func recordRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := r.Context().Value(requestInfoKey{}).(*requestInfo)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		info.remote = r.RemoteAddr
		info.device = auth.FromContext(r.Context()).Device.ID
		next.ServeHTTP(w, r)
		info.route = r.Pattern
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

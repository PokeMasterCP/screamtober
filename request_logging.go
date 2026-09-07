package main

import (
	"crypto/rand"
	"log/slog"
	"net"
	"net/http"
	"time"
)

func requestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := rand.Text()
		w.Header().Set("X-Request-ID", requestID)
		response := &loggedResponse{ResponseWriter: w}
		completed := false
		defer func() {
			status := response.status
			if status == 0 && completed {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			if !completed || status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			}
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			logger.Log(r.Context(), level, "http request",
				"request_id", requestID,
				"ip", ip,
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", float64(time.Since(started))/float64(time.Millisecond),
				"response_bytes", response.bytes,
				"aborted", !completed,
			)
		}()
		next.ServeHTTP(response, r)
		completed = true
	})
}

type loggedResponse struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *loggedResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.ResponseWriter.WriteHeader(status)
	// Informational responses other than a protocol switch are not final.
	if status >= 200 || status == http.StatusSwitchingProtocols {
		w.status = status
	}
}

func (w *loggedResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += int64(n)
	return n, err
}

// Unwrap preserves access to the underlying writer through ResponseController.
func (w *loggedResponse) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *loggedResponse) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

type requestEventKey struct{}

type requestEvent struct {
	message string
	level   slog.Level
	attrs   []any
}

// setRequestEvent enriches the completion log rather than emitting a second log.
// Call it synchronously from the handler before returning.
func setRequestEvent(r *http.Request, level slog.Level, message string, attrs ...any) {
	if event, ok := r.Context().Value(requestEventKey{}).(*requestEvent); ok {
		event.message, event.level, event.attrs = message, level, attrs
		return
	}
	// Handlers used without request middleware still report their event.
	slog.Default().With("ip", clientIP(r)).Log(r.Context(), level, message, attrs...)
}

type clientIPKey struct{}

func parseCloudflareTunnel(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("CLOUDFLARE_TUNNEL must be a boolean")
	}
	return enabled, nil
}

// Tunnel mode trusts the deployment's private ingress boundary.
func trustedClientIP(tunnel bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tunnel {
			values := r.Header.Values("CF-Connecting-IP")
			if len(values) == 1 {
				ip, err := netip.ParseAddr(strings.TrimSpace(values[0]))
				if err == nil && ip.Zone() == "" {
					r = r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip.Unmap().String()))
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok {
		return ip
	}
	return peerIP(r)
}

func peerIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func requestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := rand.Text()
		requestLog := logger.With("request_id", requestID, "ip", clientIP(r))
		event := &requestEvent{message: "http request", level: slog.LevelInfo}
		r = r.WithContext(context.WithValue(r.Context(), requestEventKey{}, event))
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
			if event.level > level {
				level = event.level
			}
			requestLog.With(event.attrs...).Log(r.Context(), level, event.message,
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

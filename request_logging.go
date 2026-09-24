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

// requestEvent accumulates one request's operation details for its completion log.
type requestEvent struct {
	level     slog.Level
	name      string
	outcome   string
	attrs     []slog.Attr
	dbQueries int
	dbTime    time.Duration
}

// recordEvent adds fields to the request's completion log. Call it synchronously
// from the handler. The level only rises, and later fields replace earlier ones
// with the same key. The first outcome is kept, so a later rendering failure
// cannot hide a committed change. Requests without logging middleware record nothing.
func recordEvent(r *http.Request, level slog.Level, outcome string, attrs ...any) {
	event, ok := r.Context().Value(requestEventKey{}).(*requestEvent)
	if !ok {
		return
	}
	event.level = max(event.level, level)
	if event.outcome == "" {
		event.outcome = outcome
	}
next:
	for _, attr := range slog.Group("", attrs...).Value.Group() {
		for i := range event.attrs {
			if event.attrs[i].Key == attr.Key {
				event.attrs[i] = attr
				continue next
			}
		}
		event.attrs = append(event.attrs, attr)
	}
}

// recordDatabase adds a database call's time, and any queries it ran, to the
// request's completion log.
func recordDatabase(ctx context.Context, started time.Time, queries int) {
	if event, ok := ctx.Value(requestEventKey{}).(*requestEvent); ok {
		event.dbQueries += queries
		event.dbTime += time.Since(started)
	}
}

// startEvent names the operation a request performs, such as "rating.save".
func startEvent(r *http.Request, name string, attrs ...any) {
	if event, ok := r.Context().Value(requestEventKey{}).(*requestEvent); ok && event.name == "" {
		event.name = name
	}
	recordEvent(r, slog.LevelInfo, "", attrs...)
}

func addEventAttrs(r *http.Request, attrs ...any) {
	recordEvent(r, slog.LevelInfo, "", attrs...)
}

func eventSucceeded(r *http.Request, attrs ...any) {
	recordEvent(r, slog.LevelInfo, "success", attrs...)
}

// eventRejected records a refused request; its response status sets the level.
func eventRejected(r *http.Request, reason string, attrs ...any) {
	recordEvent(r, slog.LevelInfo, "rejected", append([]any{"reason", reason}, attrs...)...)
}

// eventFailed records an internal failure and the step that failed.
func eventFailed(r *http.Request, step string, err error, attrs ...any) {
	recordEvent(r, slog.LevelError, "failed", append([]any{"step", step, "error", err}, attrs...)...)
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
		event := &requestEvent{level: slog.LevelInfo}
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
			var attrs []any
			if event.name != "" {
				attrs = append(attrs, "event", event.name)
			}
			if event.outcome != "" {
				attrs = append(attrs, "outcome", event.outcome)
			}
			for _, attr := range event.attrs {
				attrs = append(attrs, attr)
			}
			if event.dbQueries > 0 || event.dbTime > 0 {
				attrs = append(attrs, "db_queries", event.dbQueries, "db_ms", float64(event.dbTime)/float64(time.Millisecond))
			}
			// ServeMux records the matched pattern on this request in place. The
			// root catch-all means the request was refused before product routing.
			if r.Pattern != "" && r.Pattern != "/" {
				attrs = append(attrs, "route", r.Pattern)
			}
			requestLog.Log(r.Context(), max(level, event.level), "http request", append(attrs,
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", float64(time.Since(started))/float64(time.Millisecond),
				"response_bytes", response.bytes,
				"aborted", !completed,
			)...)
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

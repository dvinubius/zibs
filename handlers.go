package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func bearerToken(req *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(req.Header.Get("Authorization"), " ")
	return token, ok && strings.EqualFold(scheme, "Bearer") && token != ""
}

func requireBearerToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		providedToken, ok := bearerToken(req)
		validToken := ok &&
			subtle.ConstantTimeCompare([]byte(providedToken), []byte(token)) == 1
		if !validToken {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, req)
	})
}

// creationTokenUsesLeftKey carries the token uses remaining after this request
// from the authorization middleware, which spends the use, to the handler,
// which reports the remainder to the caller.
type creationTokenUsesLeftKey struct{}

// creationTokenUsesLeft reports how many uses the request's creation token has
// left. The second result is false when the request did not pass through
// requireCreationToken.
func creationTokenUsesLeft(ctx context.Context) (int, bool) {
	usesLeft, ok := ctx.Value(creationTokenUsesLeftKey{}).(int)
	return usesLeft, ok
}

func requireCreationToken(store *linkStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		token, ok := bearerToken(req)
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		usesLeft, err := store.consumeCreationToken(token)
		if err != nil {
			if errors.Is(err, ErrCreationTokenInvalid) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		ctx := context.WithValue(req.Context(), creationTokenUsesLeftKey{}, usesLeft)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

func newHandler(store *linkStore, adminToken string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", frontendPage)
	mux.Handle("GET /static/", staticAssets())
	mux.HandleFunc("GET /health", health)
	mux.Handle("POST /links", requireCreationToken(store, createLink(store)))
	mux.Handle("GET /admin/links", requireBearerToken(adminToken, getLinks(store)))
	mux.Handle("DELETE /admin/links/{code}", requireBearerToken(adminToken, deleteLinkByCode(store)))
	mux.Handle("POST /admin/tokens", requireBearerToken(adminToken, issueCreationToken(store)))
	mux.Handle("GET /admin/tokens", requireBearerToken(adminToken, getCreationTokens(store)))
	mux.Handle("DELETE /admin/tokens/{id}", requireBearerToken(adminToken, revokeCreationToken(store)))
	mux.Handle("GET /{code}", followLink(store))
	return mux
}

func routeLabel(req *http.Request) string {
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/":
		return "/"
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/static/"):
		return "/static"
	case req.Method == http.MethodGet && req.URL.Path == "/health":
		return "/health"
	case req.Method == http.MethodPost && req.URL.Path == "/links":
		return "/links"
	case req.Method == http.MethodGet && req.URL.Path == "/admin/links":
		return "/admin/links"
	case req.Method == http.MethodDelete && strings.HasPrefix(req.URL.Path, "/admin/links/"):
		return "/admin/links/{code}"
	case (req.Method == http.MethodPost || req.Method == http.MethodGet) && req.URL.Path == "/admin/tokens":
		return "/admin/tokens"
	case req.Method == http.MethodDelete && strings.HasPrefix(req.URL.Path, "/admin/tokens/"):
		return "/admin/tokens/{id}"
	default:
		return "/{code}"
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func logRequests(logger *slog.Logger, metrics *metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		metrics.httpInFlightRequests.Inc()
		defer metrics.httpInFlightRequests.Dec()

		next.ServeHTTP(recorder, req)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}

		route := routeLabel(req)
		// metrics
		metrics.httpRequests.WithLabelValues(route, req.Method, strconv.Itoa(status)).Inc()
		metrics.httpRequestDuration.WithLabelValues(route, req.Method).Observe(time.Since(started).Seconds())

		attributes := []slog.Attr{
			slog.String("event", "request_completed"),
			slog.String("route", route),
			slog.String("path", req.URL.Path),
			slog.String("method", req.Method),
			slog.Int("status", status),
			slog.Int64("duration_ms", time.Since(started).Milliseconds()),
		}
		if category := requestErrorCategory(status); category != "" {
			attributes = append(attributes, slog.String("error_category", category))
		}
		logger.LogAttrs(req.Context(), slog.LevelInfo, "request completed", attributes...)
	})
}

func requestErrorCategory(status int) string {
	switch {
	case status >= http.StatusInternalServerError:
		return "server"
	case status >= http.StatusBadRequest:
		return "client"
	default:
		return ""
	}
}

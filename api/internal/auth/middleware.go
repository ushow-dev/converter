package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const (
	claimsKey        contextKey = "jwt_claims"
	correlationIDKey contextKey = "correlation_id"
)

// JWTMiddleware returns an HTTP middleware that validates Bearer tokens.
// On failure it writes 401 JSON and aborts the chain.
func JWTMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				writeUnauthorized(w, GetCorrelationID(r.Context()))
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")
			claims, err := ParseToken(tokenStr, secret)
			if err != nil {
				writeUnauthorized(w, GetCorrelationID(r.Context()))
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// JWTQueryOrHeaderMiddleware accepts a JWT from the Authorization: Bearer header
// OR from the ?token= query parameter. Used for endpoints (e.g. thumbnail images)
// where the browser cannot set request headers.
func JWTQueryOrHeaderMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tokenStr string
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				tokenStr = strings.TrimPrefix(h, "Bearer ")
			} else {
				tokenStr = r.URL.Query().Get("token")
			}
			if tokenStr == "" {
				writeUnauthorized(w, GetCorrelationID(r.Context()))
				return
			}
			claims, err := ParseToken(tokenStr, secret)
			if err != nil {
				writeUnauthorized(w, GetCorrelationID(r.Context()))
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PlayerKeyMiddleware validates X-Player-Key header.
func PlayerKeyMiddleware(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Player-Key") != key {
				writeUnauthorized(w, GetCorrelationID(r.Context()))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClaimsFromContext retrieves JWT claims stored by JWTMiddleware.
func ClaimsFromContext(ctx context.Context) *Claims {
	v, _ := ctx.Value(claimsKey).(*Claims)
	return v
}

// CorrelationIDMiddleware injects X-Correlation-Id into context (generates one if absent).
func CorrelationIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cid := r.Header.Get("X-Correlation-Id")
		if cid == "" {
			cid = generateID()
		}
		ctx := context.WithValue(r.Context(), correlationIDKey, cid)
		w.Header().Set("X-Correlation-Id", cid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetCorrelationID retrieves the correlation ID from context.
func GetCorrelationID(ctx context.Context) string {
	if v, ok := ctx.Value(correlationIDKey).(string); ok {
		return v
	}
	return ""
}

func writeUnauthorized(w http.ResponseWriter, correlationID string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	//nolint:errcheck
	w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"authentication required","retryable":false,"correlation_id":"` + correlationID + `"}}`))
}

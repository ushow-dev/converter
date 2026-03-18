package auth

import "net/http"

// ServiceTokenMiddleware validates the X-Service-Token header for service-to-service calls.
// Returns 401 if the header is missing or does not match the expected token.
func ServiceTokenMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Service-Token") != token {
				writeUnauthorized(w, GetCorrelationID(r.Context()))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

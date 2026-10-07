// Package auth checks the bearer token on every request except the health check.
package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const maxPresented = 256

// Equal reports whether presented matches want without leaking the token length through a short-circuit compare.
func Equal(want, presented string) bool {
	if len(presented) == 0 || len(presented) > maxPresented || len(want) > maxPresented {
		dummy := make([]byte, 32)
		subtle.ConstantTimeCompare(dummy, dummy)
		return false
	}
	if len(want) != len(presented) {
		subtle.ConstantTimeCompare([]byte(want), []byte(want))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(presented)) == 1
}

// Token pulls the credential from an Authorization header. The scheme is case-sensitive.
func Token(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := header[len(prefix):]
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

// Middleware rejects missing and wrong credentials. /health is the only open route.
func Middleware(want string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		got, ok := Token(r.Header.Get("Authorization"))
		if !ok || !Equal(want, got) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("WWW-Authenticate", `Bearer realm="figures"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

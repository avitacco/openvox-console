package rbac

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type contextKey int

const claimsContextKey contextKey = iota

// ClaimsFromContext retrieves the Claims a prior Authenticate/Authorize
// call put on the request context, if any.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*Claims)
	return claims, ok
}

// WithClaims returns ctx carrying claims, as Authenticate leaves it. It
// exists for code that has to act as a caller it did not authenticate: a
// test building a request for a given set of claims.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

// Authenticate wraps h so it only runs for requests carrying a valid,
// unexpired, unrevoked token - without requiring any specific permission.
// Use this for endpoints any logged-in identity may call (e.g. logout);
// use Authorize for endpoints that need a specific permission.
func (v *Verifier) Authenticate(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := v.Verify(token)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err.Error())
			return
		}
		// An agent token is for the agent endpoint alone: anywhere else it
		// is refused, so a leaked one cannot be used against the REST API.
		if claims.Type == TokenTypeAgent && !agentTokensAllowed(r.Context()) {
			writeJSONError(w, http.StatusUnauthorized, "an agent token can only be used with the agent endpoint")
			return
		}
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		h(w, r.WithContext(ctx))
	}
}

// Authorize wraps h so it only runs for requests carrying a valid,
// unexpired, unrevoked token that also has the given permission. This is
// the function passed as `authorize` into other capabilities'
// Register(mux, authorize) methods - see design.md.
func (v *Verifier) Authorize(permission string, h http.HandlerFunc) http.HandlerFunc {
	return v.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := ClaimsFromContext(r.Context())
		if !claims.HasPermission(permission) {
			writeJSONError(w, http.StatusForbidden, "missing required permission: "+permission)
			return
		}
		h(w, r)
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimPrefix(h, prefix)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

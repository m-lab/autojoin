package handler

import (
	"context"
	"net/http"

	v0 "github.com/m-lab/autojoin/api/v0"
	v2 "github.com/m-lab/locate/api/v2"
)

type contextKey string

const orgContextKey contextKey = "organization"

// APIKeyValidator is an interface for validating API keys and retrieving
// associated organization info.
type APIKeyValidator interface {
	// ValidateKey validates the provided API key and returns the associated
	// organization name if valid. Returns an error if the key is invalid.
	ValidateKey(ctx context.Context, key string) (string, error)
}

// WithAPIKeyValidation creates middleware that validates API keys and adds
// org info to context.
func WithAPIKeyValidation(validator APIKeyValidator, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.URL.Query().Get("key")
		if apiKey == "" {
			resp := v0.RegisterResponse{
				Error: &v2.Error{
					Type:   "?key=<key>",
					Title:  "API key is required",
					Status: http.StatusUnauthorized,
				},
			}
			w.WriteHeader(resp.Error.Status)
			writeResponse(w, resp)
			return
		}

		org, err := validator.ValidateKey(r.Context(), apiKey)
		if err != nil {
			resp := v0.RegisterResponse{
				Error: &v2.Error{
					Type:   "auth.invalid_key",
					Title:  "Invalid API key",
					Status: http.StatusUnauthorized,
				},
			}
			w.WriteHeader(resp.Error.Status)
			writeResponse(w, resp)
			return
		}

		// Add org to context
		ctx := context.WithValue(r.Context(), orgContextKey, org)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

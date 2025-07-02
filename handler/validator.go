package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v4"
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
		// Use the API key from the query string, extract the organization
		// from Datastore.
		apiKey := r.URL.Query().Get("api_key")
		if apiKey == "" {
			resp := v0.RegisterResponse{
				Error: &v2.Error{
					Type:   "?api_key=<key>",
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

		ctx := context.WithValue(r.Context(), orgContextKey, org)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// validateJWTAndExtractOrg validates the JWT and extracts the "org" claim.
func validateJWTAndExtractOrg(tokenString string) (string, error) {
	// Note: This JWT *must* be verified previously in the stack, e.g. via openapi
	// security definitions.
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return "", err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if org, ok := claims["org"].(string); ok {
			return org, nil
		}
	}
	return "", errors.New("org claim not found")
}

// WithJWTValidation creates middleware that validates JWT tokens only and adds
// org info to context. This is used for the /register-jwt endpoint.
func WithJWTValidation(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check for the Authorization header.
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			resp := v0.RegisterResponse{
				Error: &v2.Error{
					Type:   "auth.missing_token",
					Title:  "JWT token is required in Authorization header",
					Status: http.StatusUnauthorized,
				},
			}
			w.WriteHeader(resp.Error.Status)
			writeResponse(w, resp)
			return
		}

		tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		org, err := validateJWTAndExtractOrg(tokenString)
		if err != nil || org == "" {
			resp := v0.RegisterResponse{
				Error: &v2.Error{
					Type:   "auth.invalid_token",
					Title:  "Invalid or missing org claim in JWT",
					Status: http.StatusUnauthorized,
				},
			}
			w.WriteHeader(resp.Error.Status)
			writeResponse(w, resp)
			return
		}

		ctx := context.WithValue(r.Context(), orgContextKey, org)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

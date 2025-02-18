package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeKeyValidator struct {
	org string
	err error
}

func (f *fakeKeyValidator) ValidateKey(ctx context.Context, key string) (string, error) {
	return f.org, f.err
}

func TestWithAPIKeyValidation(t *testing.T) {
	tests := []struct {
		name      string
		validator APIKeyValidator
		apiKey    string
		wantCode  int
		wantOrg   string
	}{
		{
			name: "success",
			validator: &fakeKeyValidator{
				org: "test-org",
				err: nil,
			},
			apiKey:   "valid-key",
			wantCode: http.StatusOK,
			wantOrg:  "test-org",
		},
		{
			name:      "error-missing-key",
			validator: &fakeKeyValidator{},
			apiKey:    "",
			wantCode:  http.StatusUnauthorized,
		},
		{
			name: "error-invalid-key",
			validator: &fakeKeyValidator{
				err: errors.New("invalid key"),
			},
			apiKey:   "invalid-key",
			wantCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotOrg string
			handler := func(w http.ResponseWriter, r *http.Request) {
				org, _ := r.Context().Value(orgContextKey).(string)
				gotOrg = org
				w.WriteHeader(http.StatusOK)
			}

			req := httptest.NewRequest("GET", "/?api_key="+tt.apiKey, nil)
			w := httptest.NewRecorder()

			WithAPIKeyValidation(tt.validator, handler)(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("WithAPIKeyValidation() status = %v, want %v", w.Code, tt.wantCode)
			}
			if tt.wantCode == http.StatusOK && gotOrg != tt.wantOrg {
				t.Errorf("WithAPIKeyValidation() org = %v, want %v", gotOrg, tt.wantOrg)
			}
		})
	}
}

package adminx

import (
	"context"
	"errors"
	"testing"
)

var errTest = errors.New("test error")

func TestAPIKeys_CreateKey(t *testing.T) {
	tests := []struct {
		name    string
		org     string
		ds      *fakeDatastore
		want    string
		wantErr bool
	}{
		{
			name: "success",
			org:  "foo",
			ds:   &fakeDatastore{},
			want: "", // The actual key will be random
		},
		{
			name:    "error",
			org:     "foo",
			ds:      &fakeDatastore{putErr: errTest},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := NewDatastoreManager(tt.ds, "test-project")
			a := NewAPIKeys(dm)
			got, err := a.CreateKey(context.Background(), tt.org)
			if (err != nil) != tt.wantErr {
				t.Errorf("APIKeys.CreateKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			// Verify key generation produces a non-empty string
			if !tt.wantErr && got == "" {
				t.Error("APIKeys.CreateKey() returned empty string, wanted non-empty")
			}
		})
	}
}

package iata

import (
	"context"
	"net/url"
	"testing"

	"github.com/m-lab/go/testingx"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		wantErr bool
	}{
		// TODO: Add test cases.
		{
			name: "success",
			file: "file:testdata/input.csv",
		},
		{
			name:    "error",
			file:    "fake-scheme:file-does-not-exist.csv",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.file)
			testingx.Must(t, err, "failed to parse file %s", tt.file)
			_, err = New(context.Background(), u)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestClient_Load(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		wantErr bool
	}{
		{
			name: "success",
			file: "file:testdata/input.csv",
		},
		{
			name: "bad-content",
			file: "file:testdata/bad.csv",
		},
		{
			name:    "bad-file",
			file:    "file:testdata/does-not-exist.csv",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.file)
			testingx.Must(t, err, "failed to parse file %s", tt.file)
			c, err := New(context.Background(), u)
			testingx.Must(t, err, "failed to create new client")

			if err := c.Load(context.Background()); (err != nil) != tt.wantErr {
				t.Errorf("Client.Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_Lookup(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		country string
		want    string
		wantErr bool
	}{
		{
			name:    "success",
			file:    "file:testdata/input.csv",
			country: "US",
			want:    "jfk",
		},
		{
			name:    "error",
			file:    "file:testdata/input.csv",
			country: "CA",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.file)
			testingx.Must(t, err, "failed to parse file %s", tt.file)
			c, err := New(context.Background(), u)
			testingx.Must(t, err, "failed to create new client")
			err = c.Load(context.Background())
			testingx.Must(t, err, "failed to load dataset")

			got, err := c.Lookup(tt.country, 40, -70)
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.Lookup() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Client.Lookup() = %v, want %v", got, tt.want)
			}
		})
	}
}

package adminx

import (
	"context"
	"fmt"
	"testing"

	"cloud.google.com/go/apikeys/apiv2/apikeyspb"
	"github.com/googleapis/gax-go/v2"
)

type fakeKeys struct {
	getKey       *apikeyspb.GetKeyStringResponse
	getKeyErr    error
	createKey    *apikeyspb.Key
	createKeyErr error
}

func (f *fakeKeys) GetKeyString(ctx context.Context, req *apikeyspb.GetKeyStringRequest, opts ...gax.CallOption) (*apikeyspb.GetKeyStringResponse, error) {
	return f.getKey, f.getKeyErr
}
func (f *fakeKeys) CreateKey(ctx context.Context, req *apikeyspb.CreateKeyRequest, opts ...gax.CallOption) (*apikeyspb.Key, error) {
	return f.createKey, f.createKeyErr
}

func TestAPIKeys_CreateKey(t *testing.T) {
	tests := []struct {
		name          string
		org           string
		locateProject string
		fakeKeys      KeysClient
		namer         *Namer
		want          string
		wantErr       bool
	}{
		{
			name:          "success-get",
			org:           "foo",
			locateProject: "mlab-foo",
			fakeKeys: &fakeKeys{
				getKey: &apikeyspb.GetKeyStringResponse{KeyString: "12345"},
			},
			namer: NewNamer("mlab-foo"),
			want:  "12345",
		},
		{
			name:          "success-create",
			org:           "foo",
			locateProject: "mlab-foo",
			fakeKeys: &fakeKeys{
				getKeyErr: createNotFoundErr(),
				createKey: &apikeyspb.Key{KeyString: "12345"},
			},
			namer: NewNamer("mlab-foo"),
			want:  "12345",
		},
		{
			name:          "error-create",
			org:           "foo",
			locateProject: "mlab-foo",
			fakeKeys: &fakeKeys{
				getKeyErr:    createNotFoundErr(),
				createKeyErr: fmt.Errorf("fake key create error"),
			},
			namer:   NewNamer("mlab-foo"),
			wantErr: true,
		},
		{
			name:          "error-other-error",
			org:           "foo",
			locateProject: "mlab-foo",
			fakeKeys: &fakeKeys{
				getKeyErr: fmt.Errorf("fake error"),
			},
			namer:   NewNamer("mlab-foo"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAPIKeys(tt.locateProject, tt.fakeKeys, tt.namer)
			got, err := a.CreateKey(context.Background(), tt.org)
			if (err != nil) != tt.wantErr {
				t.Errorf("APIKeys.CreateKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("APIKeys.CreateKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

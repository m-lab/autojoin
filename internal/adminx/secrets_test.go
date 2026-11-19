package adminx

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/googleapis/gax-go/v2"
	"github.com/googleapis/gax-go/v2/apierror"
	"google.golang.org/api/iam/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeSMC struct {
	getSecVer       *secretmanagerpb.SecretVersion
	getSecVerErr    error
	addSecVer       *secretmanagerpb.SecretVersion
	addSecVerErr    error
	accessSecVer    *secretmanagerpb.AccessSecretVersionResponse
	accessSecVerErr error
}

func (f *fakeSMC) GetSecretVersion(ctx context.Context, req *secretmanagerpb.GetSecretVersionRequest, opts ...gax.CallOption) (*secretmanagerpb.SecretVersion, error) {
	return f.getSecVer, f.getSecVerErr
}
func (f *fakeSMC) AddSecretVersion(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest, opts ...gax.CallOption) (*secretmanagerpb.SecretVersion, error) {
	return f.addSecVer, f.addSecVerErr
}
func (f *fakeSMC) AccessSecretVersion(ctx context.Context, req *secretmanagerpb.AccessSecretVersionRequest, opts ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	return f.accessSecVer, f.accessSecVerErr
}

func TestSecretManager_LoadOrCreateKey(t *testing.T) {
	tests := []struct {
		name    string
		namer   *Namer
		smc     SecretManagerClient
		iams    IAMService
		org     string
		want    string
		wantErr bool
	}{
		{
			name:  "success-load-key",
			namer: NewNamer("mlab-foo"),
			smc: &fakeSMC{
				accessSecVer: &secretmanagerpb.AccessSecretVersionResponse{
					Name: "projects/mlab-foo/secrets/fake-secret/versions/lastest",
					Payload: &secretmanagerpb.SecretPayload{
						Data: []byte("fake data"),
					},
				},
			},
			org:  "testorg",
			want: "fake data",
		},
		{
			name:  "success-create-and-store-key",
			namer: NewNamer("mlab-foo"),
			iams: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "projects/mlab-foo/secrets/fake-secret",
				},
				key: &iam.ServiceAccountKey{
					PrivateKeyData: "fake data",
				},
			},
			smc: &fakeSMC{
				accessSecVerErr: createNotFoundErr(),
				getSecVerErr:    createNotFoundErr(),
				addSecVer: &secretmanagerpb.SecretVersion{
					Name: "projects/mlab-foo/secrets/fake-secret/versions/lastest",
				},
			},
			org:  "testorg",
			want: "fake data",
		},
		{
			name:  "error-create-key",
			namer: NewNamer("mlab-foo"),
			iams: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "projects/mlab-foo/secrets/fake-secret",
				},
				keyErr: fmt.Errorf("fake error creating key"),
			},
			smc: &fakeSMC{
				accessSecVerErr: createNotFoundErr(),
				addSecVer: &secretmanagerpb.SecretVersion{
					Name: "projects/mlab-foo/secrets/fake-secret/versions/lastest",
				},
			},
			org:     "testorg",
			wantErr: true,
		},
		{
			name:  "error-store-key",
			namer: NewNamer("mlab-foo"),
			iams: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "projects/mlab-foo/secrets/fake-secret",
				},
				key: &iam.ServiceAccountKey{
					PrivateKeyData: "fake data",
				},
			},
			smc: &fakeSMC{
				accessSecVerErr: createNotFoundErr(),
				getSecVerErr:    fmt.Errorf("a different fatal error"),
			},
			org:     "testorg",
			wantErr: true,
		},
		{
			name:  "error-load-key",
			namer: NewNamer("mlab-foo"),
			smc: &fakeSMC{
				accessSecVerErr: fmt.Errorf("fake error accessing key"),
			},
			org:     "testorg",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sam := NewServiceAccountsManager(tt.iams, tt.namer)
			s := NewSecretManager(tt.smc, tt.namer, sam)
			got, err := s.LoadOrCreateKey(context.Background(), tt.org)
			if (err != nil) != tt.wantErr {
				t.Errorf("SecretManager.LoadOrCreateKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SecretManager.LoadOrCreateKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSecretManager_StoreKey(t *testing.T) {
	tests := []struct {
		name    string
		namer   *Namer
		smc     SecretManagerClient
		org     string
		key     string
		wantErr bool
	}{
		{
			name:  "success",
			namer: NewNamer("mlab-foo"),
			smc: &fakeSMC{
				getSecVerErr: createNotFoundErr(),
				addSecVer: &secretmanagerpb.SecretVersion{
					Name: "fake key name",
				},
			},
		},
		{
			name:  "error-add-secret-version-fails",
			namer: NewNamer("mlab-foo"),
			smc: &fakeSMC{
				getSecVerErr: createNotFoundErr(),
				addSecVerErr: fmt.Errorf("failed"),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sam := NewServiceAccountsManager(nil, tt.namer)
			s := NewSecretManager(tt.smc, tt.namer, sam)
			if err := s.StoreKey(context.Background(), tt.org, tt.key); (err != nil) != tt.wantErr {
				t.Errorf("SecretManager.StoreKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSecretManager_LoadOrCreateKey_NoServiceAccount(t *testing.T) {
	notFound, _ := apierror.FromError(status.Error(codes.NotFound, "fake not found"))
	n := NewNamer("mlab-foo")
	// Neither the secret nor the service account exists.
	sam := NewServiceAccountsManager(&fakeIAMService{getAcctErr: notFound}, n)
	s := NewSecretManager(&fakeSMC{accessSecVerErr: notFound}, n, sam)
	key, err := s.LoadOrCreateKey(context.Background(), "foo")
	if !errors.Is(err, ErrNoServiceAccount) {
		t.Fatalf("LoadOrCreateKey() error = %v, want ErrNoServiceAccount", err)
	}
	if key != "" {
		t.Errorf("LoadOrCreateKey() key = %q, want empty", key)
	}
}

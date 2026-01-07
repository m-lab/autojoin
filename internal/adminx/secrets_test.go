package adminx

import (
	"context"
	"fmt"
	"testing"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/api/iam/v1"
)

type fakeSMC struct {
	getSec          *secretmanagerpb.Secret
	getSecErr       error
	createSec       *secretmanagerpb.Secret
	createSecErr    error
	getSecVer       *secretmanagerpb.SecretVersion
	getSecVerErr    error
	addSecVer       *secretmanagerpb.SecretVersion
	addSecVerErr    error
	accessSecVer    *secretmanagerpb.AccessSecretVersionResponse
	accessSecVerErr error
}

func (f *fakeSMC) GetSecret(ctx context.Context, req *secretmanagerpb.GetSecretRequest, opts ...gax.CallOption) (*secretmanagerpb.Secret, error) {
	return f.getSec, f.getSecErr
}
func (f *fakeSMC) CreateSecret(ctx context.Context, req *secretmanagerpb.CreateSecretRequest, opts ...gax.CallOption) (*secretmanagerpb.Secret, error) {
	return f.createSec, f.createSecErr
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

func TestSecretManager_CreateSecret(t *testing.T) {
	tests := []struct {
		name    string
		namer   *Namer
		smc     SecretManagerClient
		org     string
		wantErr bool
	}{
		{
			name:  "success-found",
			namer: NewNamer("mlab-foo"),
			smc: &fakeSMC{
				getSec: &secretmanagerpb.Secret{
					Name: "projects/mlab-foo/secrets/fake-secret",
				},
			},
		},
		{
			name:  "success-not-found",
			namer: NewNamer("mlab-foo"),
			smc: &fakeSMC{
				getSecErr: createNotFoundErr(),
				createSec: &secretmanagerpb.Secret{
					Name: "projects/mlab-foo/secrets/fake-secret",
				},
			},
		},
		{
			name:  "error-not-found-fails-to-create-secret",
			namer: NewNamer("mlab-foo"),
			smc: &fakeSMC{
				getSecErr:    createNotFoundErr(),
				createSecErr: fmt.Errorf("failed to create new secret"),
			},
			wantErr: true,
		},
		{
			name:  "error-not-found-fails-to-create-secret",
			namer: NewNamer("mlab-foo"),
			smc: &fakeSMC{
				getSecErr: fmt.Errorf("other get error"),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSecretManager(tt.smc, tt.namer, nil)
			if err := s.CreateSecret(context.Background(), tt.org); (err != nil) != tt.wantErr {
				t.Errorf("SecretManager.CreateSecret() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
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
				getSecErr:       createNotFoundErr(),
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

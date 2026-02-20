package adminx

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/googleapis/gax-go/v2/apierror"
	"google.golang.org/api/iam/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeIAMService struct {
	getAcct    *iam.ServiceAccount
	getAcctErr error

	crAcct    *iam.ServiceAccount
	crAcctErr error

	key    *iam.ServiceAccountKey
	keyErr error

	delAcctErr error
}

func (f *fakeIAMService) GetServiceAccount(ctx context.Context, saName string) (*iam.ServiceAccount, error) {
	return f.getAcct, f.getAcctErr
}
func (f *fakeIAMService) CreateServiceAccount(ctx context.Context, projName string, req *iam.CreateServiceAccountRequest) (*iam.ServiceAccount, error) {
	return f.crAcct, f.crAcctErr
}
func (f *fakeIAMService) CreateKey(ctx context.Context, saName string, req *iam.CreateServiceAccountKeyRequest) (*iam.ServiceAccountKey, error) {
	return f.key, f.keyErr
}
func (f *fakeIAMService) DeleteServiceAccount(ctx context.Context, saName string) error {
	return f.delAcctErr
}

func createNotFoundErr() error {
	err, _ := apierror.FromError(status.Error(codes.NotFound, "fake not found"))
	return err
}

func TestServiceAccountsManager_CreateServiceAccount(t *testing.T) {
	tests := []struct {
		name    string
		iams    IAMService
		org     string
		want    *iam.ServiceAccount
		wantErr bool
	}{
		{
			name: "success-create",
			iams: &fakeIAMService{
				getAcctErr: createNotFoundErr(),
				crAcct: &iam.ServiceAccount{
					Name: "fake-name",
				},
			},
			org: "foo",
			want: &iam.ServiceAccount{
				Name: "fake-name",
			},
		},
		{
			name: "success-found",
			iams: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "fake-name",
				},
			},
			org: "foo",
			want: &iam.ServiceAccount{
				Name: "fake-name",
			},
		},
		{
			name:    "error-org-too-long",
			org:     "this-is-an-orgname-that-is-way-too-long",
			wantErr: true,
		},
		{
			name: "error-get-account-not-found",
			iams: &fakeIAMService{
				getAcctErr: createNotFoundErr(),
				crAcctErr:  fmt.Errorf("fake create error"),
			},
			org:     "foo",
			wantErr: true,
		},
		{
			name: "error-get-account-other-failure",
			iams: &fakeIAMService{
				getAcctErr: fmt.Errorf("fake get error"),
			},
			org:     "foo",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewNamer("mlab-foo")
			s := NewServiceAccountsManager(tt.iams, n)
			got, err := s.CreateServiceAccount(context.Background(), tt.org)
			if (err != nil) != tt.wantErr {
				t.Errorf("ServiceAccountsManager.CreateServiceAccount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ServiceAccountsManager.CreateServiceAccount() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceAccountsManager_CreateKey(t *testing.T) {
	tests := []struct {
		name    string
		iams    IAMService
		Namer   *Namer
		org     string
		want    *iam.ServiceAccountKey
		wantErr bool
	}{
		{
			name: "success",
			iams: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "fake-name",
				},
				key: &iam.ServiceAccountKey{
					PrivateKeyData: "fake",
				},
			},
			org: "foo",
			want: &iam.ServiceAccountKey{
				PrivateKeyData: "fake",
			},
		},
		{
			name: "error-not-found",
			iams: &fakeIAMService{
				getAcctErr: createNotFoundErr(),
			},
			wantErr: true,
		},
		{
			name: "error-other-failure",
			iams: &fakeIAMService{
				getAcctErr: fmt.Errorf("fake alternate error"),
			},
			wantErr: true,
		},
		{
			name: "error-on-create",
			iams: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "fake-name",
				},
				keyErr: fmt.Errorf("fake create error"),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewNamer("mlab-foo")
			s := NewServiceAccountsManager(tt.iams, n)
			got, err := s.CreateKey(context.Background(), tt.org)
			if (err != nil) != tt.wantErr {
				t.Errorf("ServiceAccountsManager.CreateKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ServiceAccountsManager.CreateKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceAccountsManager_DeleteServiceAccount(t *testing.T) {
	tests := []struct {
		name    string
		iams    IAMService
		org     string
		wantErr bool
	}{
		{
			name: "success",
			iams: &fakeIAMService{},
			org:  "foo",
		},
		{
			name: "success-not-found",
			iams: &fakeIAMService{
				delAcctErr: createNotFoundErr(),
			},
			org: "foo",
		},
		{
			name: "error",
			iams: &fakeIAMService{
				delAcctErr: fmt.Errorf("fake delete error"),
			},
			org:     "foo",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewNamer("mlab-foo")
			s := NewServiceAccountsManager(tt.iams, n)
			err := s.DeleteServiceAccount(context.Background(), tt.org)
			if (err != nil) != tt.wantErr {
				t.Errorf("ServiceAccountsManager.DeleteServiceAccount() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

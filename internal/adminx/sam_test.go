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
	geterr  error
	getAcct *iam.ServiceAccount

	crerr  error
	crAcct *iam.ServiceAccount

	keyerr error
	key    *iam.ServiceAccountKey
}

func (f *fakeIAMService) GetServiceAccount(ctx context.Context, saName string) (*iam.ServiceAccount, error) {
	return f.getAcct, f.geterr
}
func (f *fakeIAMService) CreateServiceAccount(ctx context.Context, projName string, req *iam.CreateServiceAccountRequest) (*iam.ServiceAccount, error) {
	return f.crAcct, f.crerr
}
func (f *fakeIAMService) CreateKey(ctx context.Context, saName string, req *iam.CreateServiceAccountKeyRequest) (*iam.ServiceAccountKey, error) {
	return f.key, f.keyerr
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
				geterr: createNotFoundErr(),
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
				geterr: createNotFoundErr(),
				crerr:  fmt.Errorf("fake create error"),
			},
			org:     "foo",
			wantErr: true,
		},
		{
			name: "error-get-account-other-failure",
			iams: &fakeIAMService{
				geterr: fmt.Errorf("fake get error"),
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
	type fields struct {
		iams  IAMService
		Namer *Namer
	}
	type args struct {
		ctx context.Context
		org string
	}
	tests := []struct {
		name  string
		iams  IAMService
		Namer *Namer
		org   string
		// want    *iam.ServiceAccount
		want    *iam.ServiceAccountKey
		wantErr bool
	}{
		// TODO: Add test cases.
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
				geterr: createNotFoundErr(),
			},
			wantErr: true,
		},
		{
			name: "error-other-failure",
			iams: &fakeIAMService{
				geterr: fmt.Errorf("fake alternate error"),
			},
			wantErr: true,
		},
		{
			name: "error-on-create",
			iams: &fakeIAMService{
				getAcct: &iam.ServiceAccount{
					Name: "fake-name",
				},
				keyerr: fmt.Errorf("fake create error"),
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

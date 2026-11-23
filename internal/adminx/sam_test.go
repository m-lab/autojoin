package adminx

import (
	"context"
	"errors"
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

	key    *iam.ServiceAccountKey
	keyErr error
}

func (f *fakeIAMService) GetServiceAccount(ctx context.Context, saName string) (*iam.ServiceAccount, error) {
	return f.getAcct, f.getAcctErr
}
func (f *fakeIAMService) CreateKey(ctx context.Context, saName string, req *iam.CreateServiceAccountKeyRequest) (*iam.ServiceAccountKey, error) {
	return f.key, f.keyErr
}

func createNotFoundErr() error {
	err, _ := apierror.FromError(status.Error(codes.NotFound, "fake not found"))
	return err
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

func TestServiceAccountsManager_CreateKey_NoServiceAccount(t *testing.T) {
	notFound, _ := apierror.FromError(status.Error(codes.NotFound, "fake not found"))
	s := NewServiceAccountsManager(&fakeIAMService{getAcctErr: notFound}, NewNamer("mlab-foo"))
	_, err := s.CreateKey(context.Background(), "foo")
	if !errors.Is(err, ErrNoServiceAccount) {
		t.Errorf("CreateKey() error = %v, want ErrNoServiceAccount", err)
	}
	if !errIsNotFound(err) {
		t.Errorf("CreateKey() error = %v, want it to still be a not found error", err)
	}
	// A failure other than a missing account is not ErrNoServiceAccount.
	s = NewServiceAccountsManager(&fakeIAMService{getAcctErr: errors.New("fake failure")}, NewNamer("mlab-foo"))
	_, err = s.CreateKey(context.Background(), "foo")
	if err == nil || errors.Is(err, ErrNoServiceAccount) {
		t.Errorf("CreateKey() error = %v, want an error that is not ErrNoServiceAccount", err)
	}
}

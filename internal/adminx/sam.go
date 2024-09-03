package adminx

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/googleapis/gax-go/v2/apierror"
	"google.golang.org/api/iam/v1"
	"google.golang.org/grpc/codes"
)

// IAMService defines the interface used to access the Google Cloud IAM Service.
type IAMService interface {
	GetServiceAccount(ctx context.Context, saName string) (*iam.ServiceAccount, error)
	CreateServiceAccount(ctx context.Context, projName string, req *iam.CreateServiceAccountRequest) (*iam.ServiceAccount, error)
	CreateKey(ctx context.Context, saName string, req *iam.CreateServiceAccountKeyRequest) (*iam.ServiceAccountKey, error)
}

// ServiceAccountsManager contains resources needed for managing service accounts.
type ServiceAccountsManager struct {
	iams  IAMService
	Namer *Namer
}

// NewServiceAccountsManager creates a new ServiceAccountManager instance.
func NewServiceAccountsManager(ic IAMService, n *Namer) *ServiceAccountsManager {
	return &ServiceAccountsManager{
		iams:  ic,
		Namer: n,
	}
}

// CreateServiceAccount returns a new service account for the given org. If the
// SA already exists, the existing resource is returned.
func (s *ServiceAccountsManager) CreateServiceAccount(ctx context.Context, org string) (*iam.ServiceAccount, error) {
	id := s.Namer.GetServiceAccountID(org)
	if len(id) > 30 {
		log.Printf("Service account id is too long: %q", s.Namer.GetServiceAccountID(org))
		return nil, fmt.Errorf("CreateServiceAccount: service account id is too long: %q", id)
	}

	// Make sure that a Service Account exists for this organization.
	// NOTE: Keys for this Service Account are created separately.
	account, err := s.iams.GetServiceAccount(ctx, s.Namer.GetServiceAccountName(org))
	switch {
	case errIsNotFound(err):
		log.Printf("Creating service account: %q", s.Namer.GetServiceAccountName(org))
		req := &iam.CreateServiceAccountRequest{
			AccountId: id,
			ServiceAccount: &iam.ServiceAccount{
				Description: "Access to GCS for measurement data for " + org,
				DisplayName: id,
			},
		}
		account, err = s.iams.CreateServiceAccount(ctx, s.Namer.GetProjectsName(), req)
		if err != nil {
			log.Printf("CreateServiceAccount failed for %q: %v", s.Namer.GetServiceAccountName(org), err)
			return nil, fmt.Errorf("CreateServiceAccount: %w", err)
		}
	case err != nil:
		log.Printf("CreateServiceAccount failed to lookup %q: %v", s.Namer.GetServiceAccountName(org), err)
		return nil, err
	}
	return account, nil
}

// CreateKey creates and returns a key for the service account associated with org.
func (s *ServiceAccountsManager) CreateKey(ctx context.Context, org string) (*iam.ServiceAccountKey, error) {
	// Get Service Account, which should have been setup during Org registration.
	account, err := s.iams.GetServiceAccount(ctx, s.Namer.GetServiceAccountName(org))
	switch {
	case errIsNotFound(err):
		log.Printf("Service account does not exist! Org setup may be incomplete: %v", err)
		return nil, err
	case err != nil:
		log.Printf("Error prevented finding service account to create key: %v", err)
		return nil, err
	}

	// Add key to service account.
	keyreq := &iam.CreateServiceAccountKeyRequest{
		KeyAlgorithm:   "KEY_ALG_RSA_2048",
		PrivateKeyType: "TYPE_GOOGLE_CREDENTIALS_FILE",
	}
	log.Printf("Creating service account key: %q", account.Name)
	key, err := s.iams.CreateKey(ctx, account.Name, keyreq)
	if err != nil {
		log.Printf("CreateKey failed for %q: %v", account.Name, err)
		return nil, fmt.Errorf("CreateKey(%s): %w", account.Name, err)
	}
	return key, nil
}

func errIsNotFound(err error) bool {
	var gerr *apierror.APIError
	if errors.As(err, &gerr) {
		s := gerr.GRPCStatus()
		return (s != nil && s.Code() == codes.NotFound) || gerr.HTTPCode() == http.StatusNotFound
	}
	return false
}

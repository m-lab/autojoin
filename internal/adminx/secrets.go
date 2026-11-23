package adminx

import (
	"context"
	"errors"
	"log"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/googleapis/gax-go/v2"
)

// SecretManagerClient is an interface describing operations on the Google Cloud Secret Manager API.
type SecretManagerClient interface {
	GetSecretVersion(ctx context.Context, req *secretmanagerpb.GetSecretVersionRequest, opts ...gax.CallOption) (*secretmanagerpb.SecretVersion, error)
	AddSecretVersion(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest, opts ...gax.CallOption) (*secretmanagerpb.SecretVersion, error)
	AccessSecretVersion(ctx context.Context, req *secretmanagerpb.AccessSecretVersionRequest, opts ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error)
}

// SecretManager manages operations on secrets.
type SecretManager struct {
	Namer   *Namer
	smc     SecretManagerClient
	sam     *ServiceAccountsManager
	version string
}

// NewSecretManager creates a new secret manager instance.
func NewSecretManager(smc SecretManagerClient, n *Namer, sam *ServiceAccountsManager) *SecretManager {
	return &SecretManager{
		Namer:   n,
		smc:     smc,
		sam:     sam,
		version: "latest",
	}
}

// LoadOrCreateKey is a single method to either create and store a key or
// read an existing key from SecretManager.
func (s *SecretManager) LoadOrCreateKey(ctx context.Context, org string) (string, error) {
	key, err := s.LoadKey(ctx, org)
	switch {
	case errIsNotFound(err):
		k, err := s.sam.CreateKey(ctx, org)
		if err != nil {
			// An org without a service account is expected: it uses workload
			// identity federation. The caller decides what to do about it.
			if !errors.Is(err, ErrNoServiceAccount) {
				log.Printf("CreateKey failed for %q: %v", s.Namer.GetServiceAccountName(org), err)
			}
			return "", err
		}
		// Store the new key in secret manager.
		// NOTE: key is already base64 encoded.
		err = s.StoreKey(ctx, org, k.PrivateKeyData)
		if err != nil {
			log.Printf("StoreKey failed for %q: %v", s.Namer.GetServiceAccountName(org), err)
			return "", err
		}
		key = k.PrivateKeyData
	case err != nil:
		log.Printf("Loadkey failed for %q: %v", s.Namer.GetServiceAccountName(org), err)
		return "", err
	}
	return key, nil
}

// StoreKey saves the given key in the org's secret.
func (s *SecretManager) StoreKey(ctx context.Context, org string, key string) error {
	// Declare the payload to store.
	payload := []byte(key)
	req := &secretmanagerpb.GetSecretVersionRequest{
		Name: s.Namer.GetSecretName(org) + "/versions/" + s.version,
	}
	// NOTE: once a secret is created it will not be overwritten. It must be deleted first.
	version, err := s.smc.GetSecretVersion(ctx, req)
	switch {
	case errIsNotFound(err):
		// Add secret.
		addReq := &secretmanagerpb.AddSecretVersionRequest{
			Parent: s.Namer.GetSecretName(org),
			Payload: &secretmanagerpb.SecretPayload{
				Data: payload,
			},
		}
		version, err = s.smc.AddSecretVersion(ctx, addReq)
		if err != nil {
			return err
		}
		log.Println("Added version:", version.Name)
	case err != nil:
		return err
	}
	log.Println("Stored:", version.Name)
	return nil
}

// LoadKey loads a key from the org's secret. LoadKey returns error if the key is not found.
func (s *SecretManager) LoadKey(ctx context.Context, org string) (string, error) {
	// Build the request.
	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: s.Namer.GetSecretName(org) + "/versions/" + s.version,
	}
	// Call the API.
	result, err := s.smc.AccessSecretVersion(ctx, req)
	if err != nil {
		return "", err
	}
	return string(result.Payload.Data), nil
}

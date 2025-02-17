package adminx

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/datastore"
)

const autojoinNamespace = "autojoin"
const OrgKind = "Organization"
const APIKeyKind = "APIKey"

var (
	// ErrInvalidKey is returned when the API key is not found in Datastore
	ErrInvalidKey = errors.New("invalid API key")
)

type DatastoreClient interface {
	Put(ctx context.Context, key *datastore.Key, src interface{}) (*datastore.Key, error)
	Get(ctx context.Context, key *datastore.Key, dst interface{}) error
	GetAll(ctx context.Context, q *datastore.Query, dst interface{}) ([]*datastore.Key, error)
}

// Organization represents a Datastore entity for storing organization metadata
type Organization struct {
	Name      string    `datastore:"name"`
	Email     string    `datastore:"email"`
	CreatedAt time.Time `datastore:"created_at"`
}

// APIKey represents a Datastore entity for storing API key metadata.
type APIKey struct {
	CreatedAt time.Time `datastore:"created_at"`
}

// DatastoreOrgManager maintains state for managing organizations and API keys in Datastore
type DatastoreOrgManager struct {
	client    DatastoreClient
	project   string
	namespace string
}

// Add constructor
func NewDatastoreManager(client DatastoreClient, project string) *DatastoreOrgManager {
	return &DatastoreOrgManager{
		client:    client,
		project:   project,
		namespace: autojoinNamespace,
	}
}

// Add CreateOrganization method
func (d *DatastoreOrgManager) CreateOrganization(ctx context.Context, name, email string) error {
	key := datastore.NameKey(OrgKind, name, nil)
	key.Namespace = d.namespace

	org := &Organization{
		Name:      name,
		Email:     email,
		CreatedAt: time.Now().UTC(),
	}

	_, err := d.client.Put(ctx, key, org)
	return err
}

// CreateAPIKey creates a new API key as a child entity of the organization
func (d *DatastoreOrgManager) CreateAPIKey(ctx context.Context, org string) (string, error) {
	parentKey := datastore.NameKey(OrgKind, org, nil)
	parentKey.Namespace = d.namespace

	// Generate random API key
	keyString, err := GenerateAPIKey()
	if err != nil {
		return "", err
	}

	// Use the generated string as the key name
	key := datastore.NameKey(APIKeyKind, keyString, parentKey)
	key.Namespace = d.namespace

	apiKey := &APIKey{
		CreatedAt: time.Now().UTC(),
	}

	_, err = d.client.Put(ctx, key, apiKey)
	if err != nil {
		return "", err
	}

	return keyString, nil
}

// GetAPIKeys retrieves all API keys for an organization
func (d *DatastoreOrgManager) GetAPIKeys(ctx context.Context, org string) ([]string, error) {
	parentKey := datastore.NameKey(OrgKind, org, nil)
	parentKey.Namespace = d.namespace

	q := datastore.NewQuery(APIKeyKind).Ancestor(parentKey).KeysOnly()

	keys, err := d.client.GetAll(ctx, q, nil)
	if err != nil {
		return nil, err
	}

	apiKeys := make([]string, len(keys))
	for i, key := range keys {
		apiKeys[i] = key.Name
	}

	return apiKeys, nil
}

// ValidateKey checks if the API key exists and returns the associated organization name.
func (d *DatastoreOrgManager) ValidateKey(ctx context.Context, key string) (string, error) {
	// Create the key to look up.
	apiKey := datastore.NameKey(APIKeyKind, key, nil)
	apiKey.Namespace = d.namespace

	// Try to get the entity
	var keyEntity APIKey
	err := d.client.Get(ctx, apiKey, &keyEntity)
	fmt.Printf("keyEntity: %v, err: %v\n", keyEntity, err)
	if err == datastore.ErrNoSuchEntity {
		return "", ErrInvalidKey
	}
	if err != nil {
		return "", err
	}

	// Get the parent (organization) key
	orgKey := apiKey.Parent
	fmt.Printf("orgKey: %v\n", orgKey)
	if orgKey == nil {
		return "", errors.New("API key has no parent organization")
	}

	return orgKey.Name, nil
}

// GenerateAPIKey generates a random string to be used as API key.
func GenerateAPIKey() (string, error) {
	b := make([]byte, 32) // 256 bits of randomness
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

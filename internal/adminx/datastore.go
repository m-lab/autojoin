package adminx

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"cloud.google.com/go/datastore"
)

const autojoinNamespace = "autojoin"

type DatastoreClient interface {
	Put(ctx context.Context, key *datastore.Key, src interface{}) (*datastore.Key, error)
	Get(ctx context.Context, key *datastore.Key, dst interface{}) error
	GetAll(ctx context.Context, q *datastore.Query, dst interface{}) ([]*datastore.Key, error)
}

// Organization represents a Datastore entity for storing organization metadata
type Organization struct {
	Name      string    `datastore:"name"`
	Email     string    `datastore:"contact"`
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
	key := datastore.NameKey("Organization", name, nil)
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
	parentKey := datastore.NameKey("Organization", org, nil)
	parentKey.Namespace = d.namespace

	// Generate random API key
	keyString, err := GenerateAPIKey()
	if err != nil {
		return "", err
	}

	// Use the generated string as the key name
	key := datastore.NameKey("APIKey", keyString, parentKey)
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
	parentKey := datastore.NameKey("Organization", org, nil)
	parentKey.Namespace = d.namespace

	q := datastore.NewQuery("APIKey").Ancestor(parentKey).KeysOnly()

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

// GenerateAPIKey generates a random string to be used as API key.
func GenerateAPIKey() (string, error) {
	b := make([]byte, 32) // 256 bits of randomness
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

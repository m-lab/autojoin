package adminx

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
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

// DatastoreClient is an interface for interacting with Datastore.
type DatastoreClient interface {
	Put(ctx context.Context, key *datastore.Key, src interface{}) (*datastore.Key, error)
	Get(ctx context.Context, key *datastore.Key, dst interface{}) error
	GetAll(ctx context.Context, q *datastore.Query, dst interface{}) ([]*datastore.Key, error)
}

// Organization represents a Datastore entity for storing organization metadata.
type Organization struct {
	Name                  string    `datastore:"name"`
	Email                 string    `datastore:"email"`
	CreatedAt             time.Time `datastore:"created_at"`
	ProbabilityMultiplier float64   `datastore:"probability_multiplier"`
}

// APIKey represents a Datastore entity for storing API key metadata.
type APIKey struct {
	CreatedAt time.Time `datastore:"created_at"`
	Key       string    `datastore:"key"`
}

// DatastoreOrgManager maintains state for managing organizations and API keys in Datastore.
type DatastoreOrgManager struct {
	client    DatastoreClient
	project   string
	namespace string
}

// NewDatastoreManager creates a new DatastoreOrgManager instance.
func NewDatastoreManager(client DatastoreClient, project string) *DatastoreOrgManager {
	return &DatastoreOrgManager{
		client:    client,
		project:   project,
		namespace: autojoinNamespace,
	}
}

// CreateOrganization creates a new organization entity in Datastore.
func (d *DatastoreOrgManager) CreateOrganization(ctx context.Context, name, email string) error {
	key := datastore.NameKey(OrgKind, name, nil)
	key.Namespace = d.namespace

	org := &Organization{
		Name:                  name,
		Email:                 email,
		CreatedAt:             time.Now().UTC(),
		ProbabilityMultiplier: 1.0,
	}

	_, err := d.client.Put(ctx, key, org)
	return err
}

// GetOrganization retrieves an organization by its name.
func (d *DatastoreOrgManager) GetOrganization(ctx context.Context, orgName string) (*Organization, error) {
	key := datastore.NameKey(OrgKind, orgName, nil)
	key.Namespace = d.namespace

	var org Organization
	err := d.client.Get(ctx, key, &org)
	if err != nil {
		return nil, err
	}

	return &org, nil
}

// CreateAPIKeyWithValue creates a new API key as a child entity of the organization.
func (d *DatastoreOrgManager) CreateAPIKeyWithValue(ctx context.Context, org, value string) (string, error) {
	parentKey := datastore.NameKey(OrgKind, org, nil)
	parentKey.Namespace = d.namespace

	// Use the generated string as the key name
	key := datastore.NameKey(APIKeyKind, value, parentKey)
	key.Namespace = d.namespace

	apiKey := &APIKey{
		CreatedAt: time.Now().UTC(),
		Key:       value,
	}

	resKey, err := d.client.Put(ctx, key, apiKey)
	if err != nil {
		return "", err
	}

	return resKey.Name, nil
}

// GetAPIKeys retrieves all API keys for an organization
func (d *DatastoreOrgManager) GetAPIKeys(ctx context.Context, org string) ([]string, error) {
	parentKey := datastore.NameKey(OrgKind, org, nil)
	parentKey.Namespace = d.namespace

	q := datastore.NewQuery(APIKeyKind).
		Namespace(d.namespace).
		Ancestor(parentKey).
		KeysOnly()

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
	q := datastore.NewQuery(APIKeyKind).
		Namespace(d.namespace).
		FilterField("key", "=", key).Limit(1)

	var keys []*datastore.Key
	var entities []APIKey
	keys, err := d.client.GetAll(ctx, q, &entities)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", ErrInvalidKey
	}

	// Get the parent (organization) key from the first result
	orgKey := keys[0].Parent
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

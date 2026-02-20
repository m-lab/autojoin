package adminx

import (
	"context"
	"errors"

	"cloud.google.com/go/datastore"
	"github.com/m-lab/token-exchange/store"
)

// datastoreClient defines datastore operations needed by orgadm setup/delete.
type datastoreClient interface {
	Put(ctx context.Context, key *datastore.Key, src any) (*datastore.Key, error)
	Get(ctx context.Context, key *datastore.Key, dst any) error
	GetAll(ctx context.Context, q *datastore.Query, dst any) ([]*datastore.Key, error)
	Delete(ctx context.Context, key *datastore.Key) error
	DeleteMulti(ctx context.Context, keys []*datastore.Key) error
}

// DatastoreManager wraps token-exchange AutojoinManager and adds delete helpers.
type DatastoreManager struct {
	am        *store.AutojoinManager
	client    datastoreClient
	namespace string
}

// NewDatastoreManager creates a DatastoreManager for setup and teardown.
func NewDatastoreManager(client datastoreClient, project, namespace string) *DatastoreManager {
	return &DatastoreManager{
		am:        store.NewAutojoinManager(client, project, namespace),
		client:    client,
		namespace: namespace,
	}
}

// CreateOrganization creates a new organization entity in datastore.
func (d *DatastoreManager) CreateOrganization(ctx context.Context, name, email string) error {
	return d.am.CreateOrganization(ctx, name, email)
}

// CreateAPIKeyWithValue creates a new API key for org.
func (d *DatastoreManager) CreateAPIKeyWithValue(ctx context.Context, org, value string) (string, error) {
	return d.am.CreateAPIKeyWithValue(ctx, org, value)
}

// GetAPIKeys returns all API keys for org.
func (d *DatastoreManager) GetAPIKeys(ctx context.Context, org string) ([]string, error) {
	return d.am.GetAPIKeys(ctx, org)
}

// DeleteAPIKeys deletes all API key entities under Organization/<org>.
func (d *DatastoreManager) DeleteAPIKeys(ctx context.Context, org string) error {
	parent := datastore.NameKey(store.AutojoinOrgKind, org, nil)
	parent.Namespace = d.namespace

	q := datastore.NewQuery(store.AutojoinAPIKeyKind).
		Namespace(d.namespace).
		Ancestor(parent).
		KeysOnly()

	keys, err := d.client.GetAll(ctx, q, nil)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	err = d.client.DeleteMulti(ctx, keys)
	if datastoreDeleteNotFound(err) {
		return nil
	}
	return err
}

// DeleteOrganization deletes Organization/<org>.
func (d *DatastoreManager) DeleteOrganization(ctx context.Context, org string) error {
	key := datastore.NameKey(store.AutojoinOrgKind, org, nil)
	key.Namespace = d.namespace
	err := d.client.Delete(ctx, key)
	if errors.Is(err, datastore.ErrNoSuchEntity) {
		return nil
	}
	return err
}

func datastoreDeleteNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, datastore.ErrNoSuchEntity) {
		return true
	}
	var merr datastore.MultiError
	if errors.As(err, &merr) {
		for i := range merr {
			if merr[i] != nil && !errors.Is(merr[i], datastore.ErrNoSuchEntity) {
				return false
			}
		}
		return true
	}
	return false
}

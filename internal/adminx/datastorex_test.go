package adminx

import (
	"context"
	"errors"
	"testing"

	"cloud.google.com/go/datastore"
	"github.com/m-lab/token-exchange/store"
)

type fakeDatastoreClient struct {
	getAllKeys      []*datastore.Key
	getAllErr       error
	deleteErr       error
	deleteMultiErr  error
	deletedKey      *datastore.Key
	deletedKeyBatch []*datastore.Key
}

func (f *fakeDatastoreClient) Put(ctx context.Context, key *datastore.Key, src any) (*datastore.Key, error) {
	return key, nil
}

func (f *fakeDatastoreClient) Get(ctx context.Context, key *datastore.Key, dst any) error {
	return nil
}

func (f *fakeDatastoreClient) GetAll(ctx context.Context, q *datastore.Query, dst any) ([]*datastore.Key, error) {
	return f.getAllKeys, f.getAllErr
}

func (f *fakeDatastoreClient) Delete(ctx context.Context, key *datastore.Key) error {
	f.deletedKey = key
	return f.deleteErr
}

func (f *fakeDatastoreClient) DeleteMulti(ctx context.Context, keys []*datastore.Key) error {
	f.deletedKeyBatch = append([]*datastore.Key{}, keys...)
	return f.deleteMultiErr
}

func TestDatastoreManager_DeleteAPIKeys(t *testing.T) {
	keys := []*datastore.Key{
		datastore.NameKey(store.AutojoinAPIKeyKind, "k1", datastore.NameKey(store.AutojoinOrgKind, "foo", nil)),
		datastore.NameKey(store.AutojoinAPIKeyKind, "k2", datastore.NameKey(store.AutojoinOrgKind, "foo", nil)),
	}

	tests := []struct {
		name    string
		client  *fakeDatastoreClient
		wantDel int
		wantErr bool
	}{
		{
			name: "success",
			client: &fakeDatastoreClient{
				getAllKeys: keys,
			},
			wantDel: 2,
		},
		{
			name: "success-no-keys",
			client: &fakeDatastoreClient{
				getAllKeys: nil,
			},
			wantDel: 0,
		},
		{
			name: "success-not-found",
			client: &fakeDatastoreClient{
				getAllKeys:     keys,
				deleteMultiErr: datastore.MultiError{datastore.ErrNoSuchEntity, nil},
			},
			wantDel: 2,
		},
		{
			name: "error-getall",
			client: &fakeDatastoreClient{
				getAllErr: errors.New("getall"),
			},
			wantErr: true,
		},
		{
			name: "error-delete-multi",
			client: &fakeDatastoreClient{
				getAllKeys:     keys,
				deleteMultiErr: errors.New("delete-multi"),
			},
			wantDel: 2,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := NewDatastoreManager(tt.client, "mlab-foo", "platform-credentials")
			err := dm.DeleteAPIKeys(context.Background(), "foo")
			if (err != nil) != tt.wantErr {
				t.Fatalf("DatastoreManager.DeleteAPIKeys() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got := len(tt.client.deletedKeyBatch); got != tt.wantDel {
				t.Fatalf("DatastoreManager.DeleteAPIKeys() deleted = %d, want %d", got, tt.wantDel)
			}
		})
	}
}

func TestDatastoreManager_DeleteOrganization(t *testing.T) {
	tests := []struct {
		name    string
		client  *fakeDatastoreClient
		wantErr bool
	}{
		{
			name:   "success",
			client: &fakeDatastoreClient{},
		},
		{
			name: "success-not-found",
			client: &fakeDatastoreClient{
				deleteErr: datastore.ErrNoSuchEntity,
			},
		},
		{
			name: "error",
			client: &fakeDatastoreClient{
				deleteErr: errors.New("delete"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := NewDatastoreManager(tt.client, "mlab-foo", "platform-credentials")
			err := dm.DeleteOrganization(context.Background(), "foo")
			if (err != nil) != tt.wantErr {
				t.Fatalf("DatastoreManager.DeleteOrganization() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.client.deletedKey == nil {
				t.Fatalf("DatastoreManager.DeleteOrganization() did not call Delete")
			}
		})
	}
}

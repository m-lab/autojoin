package adminx

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloud.google.com/go/datastore"
)

var errTest = errors.New("test error")

type fakeDatastore struct {
	// Map of key name to (entity, parent key) pair
	keys map[string]struct {
		entity *APIKey
		parent *datastore.Key
	}
	putErr error
	getErr error
}

func (f *fakeDatastore) Put(ctx context.Context, key *datastore.Key, src interface{}) (*datastore.Key, error) {
	return key, f.putErr
}
func (f *fakeDatastore) Get(ctx context.Context, key *datastore.Key, dst interface{}) error {
	if f.getErr != nil {
		return f.getErr
	}

	if f.keys != nil {
		if entry, exists := f.keys[key.Name]; exists {
			apiKey := dst.(*APIKey)
			*apiKey = *entry.entity
			key.Parent = entry.parent
			return nil
		}
		return datastore.ErrNoSuchEntity
	}

	return f.getErr
}

func (f *fakeDatastore) GetAll(ctx context.Context, q *datastore.Query, dst interface{}) ([]*datastore.Key, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}

	// If we have test data, return it
	if f.keys != nil {
		// Get the destination slice
		entities := dst.(*[]APIKey)
		keys := []*datastore.Key{}

		// Add each test entity to the results
		for keyName, entry := range f.keys {
			*entities = append(*entities, *entry.entity)
			key := datastore.NameKey(APIKeyKind, keyName, entry.parent)
			keys = append(keys, key)
		}
		return keys, nil
	}

	// Empty results if no test data
	return []*datastore.Key{}, nil
}

func TestDatastoreOrgManager_ValidateKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		ds      *fakeDatastore
		wantOrg string
		wantErr error
	}{
		{
			name: "success",
			key:  "valid-key",
			ds: &fakeDatastore{
				keys: map[string]struct {
					entity *APIKey
					parent *datastore.Key
				}{
					"valid-key": {
						entity: &APIKey{CreatedAt: time.Now()},
						parent: datastore.NameKey(OrgKind, "test-org", nil),
					},
				},
			},
			wantOrg: "test-org",
		},
		{
			name: "error-invalid-key",
			key:  "invalid-key",
			ds: &fakeDatastore{
				keys: map[string]struct {
					entity *APIKey
					parent *datastore.Key
				}{},
			},
			wantErr: ErrInvalidKey,
		},
		{
			name: "error-datastore",
			key:  "valid-key",
			ds: &fakeDatastore{
				getErr: errTest,
			},
			wantErr: errTest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := NewDatastoreManager(tt.ds, "test-project")
			gotOrg, err := dm.ValidateKey(context.Background(), tt.key)

			if (err != nil && tt.wantErr == nil) ||
				(err == nil && tt.wantErr != nil) ||
				(err != nil && tt.wantErr != nil && err.Error() != tt.wantErr.Error()) {
				t.Errorf("ValidateKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if gotOrg != tt.wantOrg {
				t.Errorf("ValidateKey() = %v, want %v", gotOrg, tt.wantOrg)
			}
		})
	}
}

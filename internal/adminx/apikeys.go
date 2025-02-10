package adminx

import (
	"context"
)

// APIKeys maintains state for allocating API keys.
type APIKeys struct {
	ds *DatastoreOrgManager
}

// NewAPIKeys creates a new APIKeys instance for allocating API keys.
func NewAPIKeys(ds *DatastoreOrgManager) *APIKeys {
	return &APIKeys{
		ds: ds,
	}
}

// CreateKey returns an API key for use by the named org.
// CreateKey can be called multiple times safely.
func (a *APIKeys) CreateKey(ctx context.Context, org string) (string, error) {
	return a.ds.CreateAPIKey(ctx, org)
}

package adminx

import (
	"context"

	"cloud.google.com/go/apikeys/apiv2/apikeyspb"
	"github.com/googleapis/gax-go/v2"
)

// KeysClient defines the interface used by the APIKeys type to allocate API keys.
type KeysClient interface {
	GetKeyString(ctx context.Context, req *apikeyspb.GetKeyStringRequest, opts ...gax.CallOption) (*apikeyspb.GetKeyStringResponse, error)
	CreateKey(ctx context.Context, req *apikeyspb.CreateKeyRequest, opts ...gax.CallOption) (*apikeyspb.Key, error)
}

// APIKeys maintains state for allcoating API keys.
type APIKeys struct {
	locateProject string
	client        KeysClient
	namer         *Namer
}

// NewAPIKeys creates a new APIKeys instance for allocating API keys.
func NewAPIKeys(locateProj string, c KeysClient, n *Namer) *APIKeys {
	return &APIKeys{
		locateProject: locateProj,
		client:        c,
		namer:         n,
	}
}

// CreateKey returns an API key restricted to the Locate and Autojoin APIs for use by the named org.
// CreateKey can be called multiple times safely.
func (a *APIKeys) CreateKey(ctx context.Context, org string) (string, error) {
	// Attempt to get the api key by name to see if it already exists.
	get, err := a.client.GetKeyString(ctx, &apikeyspb.GetKeyStringRequest{
		Name: a.namer.GetAPIKeyName(org),
	})
	if errIsNotFound(err) {
		// If the key does not yet exist, create it.
		// While not documented, it appears to be safe to run this operation multiple times.
		key, err := a.client.CreateKey(ctx, &apikeyspb.CreateKeyRequest{
			Parent: a.namer.GetAPIKeyParent(),
			Key: &apikeyspb.Key{
				DisplayName: a.namer.GetAPIKeyID(org),
				Restrictions: &apikeyspb.Restrictions{
					ApiTargets: []*apikeyspb.ApiTarget{
						{Service: "autojoin-dot-" + a.namer.Project + ".appspot.com"},
						{Service: "locate-dot-" + a.locateProject + ".appspot.com"},
					},
				},
			},
			KeyId: a.namer.GetAPIKeyID(org),
		})
		if err != nil {
			return "", err
		}
		return key.KeyString, nil
	}
	if err != nil {
		return "", err
	}
	return get.KeyString, nil
}

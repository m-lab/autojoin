package keysiface

import (
	"context"

	apikeys "cloud.google.com/go/apikeys/apiv2"
	"cloud.google.com/go/apikeys/apiv2/apikeyspb"
	"github.com/googleapis/gax-go"
)

type keysImpl struct {
	client *apikeys.Client
}

func NewKeys(k *apikeys.Client) *keysImpl {
	return &keysImpl{
		client: k,
	}
}

func (c *keysImpl) GetKeyString(ctx context.Context, req *apikeyspb.GetKeyStringRequest, opts ...gax.CallOption) (*apikeyspb.GetKeyStringResponse, error) {
	return c.client.GetKeyString(ctx, req)
}

func (c *keysImpl) CreateKey(ctx context.Context, req *apikeyspb.CreateKeyRequest, opts ...gax.CallOption) (*apikeyspb.Key, error) {
	key, err := c.client.CreateKey(ctx, req)
	if err != nil {
		return nil, err
	}
	// Wait for the create operation to complete.
	return key.Wait(ctx)
}

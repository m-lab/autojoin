package iamiface

import (
	"context"

	"google.golang.org/api/iam/v1"
)

type iamImpl struct {
	iamClient *iam.Service
}

func NewIAM(ic *iam.Service) *iamImpl {
	return &iamImpl{
		iamClient: ic,
	}
}

func (i *iamImpl) GetServiceAccount(ctx context.Context, saName string) (*iam.ServiceAccount, error) {
	return i.iamClient.Projects.ServiceAccounts.Get(saName).Context(ctx).Do()
}

func (i *iamImpl) CreateServiceAccount(ctx context.Context, pName string, req *iam.CreateServiceAccountRequest) (*iam.ServiceAccount, error) {
	return i.iamClient.Projects.ServiceAccounts.Create(pName, req).Context(ctx).Do()
}

func (i *iamImpl) CreateKey(ctx context.Context, saName string, req *iam.CreateServiceAccountKeyRequest) (*iam.ServiceAccountKey, error) {
	return i.iamClient.Projects.ServiceAccounts.Keys.Create(saName, req).Context(ctx).Do()
}

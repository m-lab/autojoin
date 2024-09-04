package crmiface

import (
	"context"

	"google.golang.org/api/cloudresourcemanager/v1"
)

type crmImpl struct {
	crm     *cloudresourcemanager.Service
	Project string
}

// NewCRM creates a new crm implementation for wrapping the cloudresourcemanager.Service.
func NewCRM(project string, crm *cloudresourcemanager.Service) *crmImpl {
	return &crmImpl{
		Project: project,
		crm:     crm,
	}
}

func (c *crmImpl) GetIamPolicy(ctx context.Context, req *cloudresourcemanager.GetIamPolicyRequest) (*cloudresourcemanager.Policy, error) {
	return c.crm.Projects.GetIamPolicy(c.Project, req).Context(ctx).Do()
}

func (c *crmImpl) SetIamPolicy(ctx context.Context, req *cloudresourcemanager.SetIamPolicyRequest) error {
	_, err := c.crm.Projects.SetIamPolicy(c.Project, req).Context(ctx).Do()
	return err
}

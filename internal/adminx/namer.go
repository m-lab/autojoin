package adminx

import (
	"strings"
)

// Namer contains metadata needed for resource naming.
type Namer struct {
	Project string
}

// NewNamer creates a new Namer instance for the given project.
func NewNamer(proj string) *Namer {
	return &Namer{Project: proj}
}

// GetProjectsPrefix returns a google cloud project resource name,
// e.g. projects/mlab-foo
func (n *Namer) GetProjectsName() string {
	return "projects/" + n.Project
}

// GetServiceAccountID returns a service account ID for this org, e.g.
// autonode-org, replacing all dots with dashes in the org name.
func (n *Namer) GetServiceAccountID(org string) string {
	return "autonode-" + strings.ReplaceAll(org, ".", "-")
}

// GetServiceAccountEmail returns a service account email for this org, e.g.
// autonode-org@mlab-foo.iam.gserviceaccount.com
func (n *Namer) GetServiceAccountEmail(org string) string {
	return n.GetServiceAccountID(org) + "@" + n.Project + ".iam.gserviceaccount.com"
}

// GetServiceAccountName returns a google cloud service account resource name,
// e.g. projects/mlab-foo/serviceAccounts/autonode-foo@mlab-foo.iam.gserviceaccount.com
func (n *Namer) GetServiceAccountName(org string) string {
	return n.GetProjectsName() + "/serviceAccounts/" + n.GetServiceAccountEmail(org)
}

// GetSecretID returns a secret ID for this org, e.g. autojoin-serviceaccount-key-org.
func (n *Namer) GetSecretID(org string) string {
	return "autojoin-serviceaccount-key-" + strings.ReplaceAll(org, ".", "-")
}

// GetSecretName returns the google cloud secret resource name, e.g.
// projects/mlab-foo/secrets/autojoin-serviceaccount-key-org
func (n *Namer) GetSecretName(org string) string {
	return n.GetProjectsName() + "/secrets/" + n.GetSecretID(org)
}

// GetAPIKeyParent returns the parent API key resource name for this project.
// e.g. projects/mlab-foo/locations/global
func (n *Namer) GetAPIKeyParent() string {
	return n.GetProjectsName() + "/locations/global"
}

// GetAPIKeyName returns the API key resource name for the given org.
// e.g. projects/mlab-foo/locations/global/keys/autojoin-key-foo
func (n *Namer) GetAPIKeyName(org string) string {
	return n.GetAPIKeyParent() + "/keys/" + n.GetAPIKeyID(org)
}

// GetAPIKeyID returns the API key resource ID for the given org.
// e.g. autojoin-key-foo
func (n *Namer) GetAPIKeyID(org string) string {
	return "autojoin-key-" + strings.ReplaceAll(org, ".", "-")
}

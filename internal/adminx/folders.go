package adminx

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/storage/v1"
)

const (
	// orgUploadRole lets a member create objects but not overwrite or delete them.
	orgUploadRole = "roles/storage.objectCreator"
)

// WorkloadIdentityPoolID is the ID of the workload identity pool that node
// storage tokens are exchanged through. It is the same in every project: the
// pool is created by terraform-support (modules/autojoin), and the autojoin
// deployment names it in the audience it returns to nodes.
const WorkloadIdentityPoolID = "autojoin"

var (
	// validOrgName matches an org name, which is a DNS label in node
	// hostnames: at most 21 lowercase letters, digits and hyphens, starting and
	// ending with a letter or digit. This excludes "/", "*", "." and every
	// other character that could change the meaning of a managed folder name
	// or an IAM principal identifier.
	validOrgName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,19}[a-z0-9])?$`)

	// validProjectNumber accepts only a decimal Google Cloud project number.
	validProjectNumber = regexp.MustCompile(`^[0-9]+$`)

	// validPoolID matches the documented workload identity pool ID format.
	validPoolID = regexp.MustCompile(`^[a-z0-9-]{4,32}$`)

	// ErrInvalidOrgName is returned when an org name is not safe to use in
	// resource names or IAM principal identifiers.
	ErrInvalidOrgName = errors.New("invalid org name")
)

// ValidateOrgName returns an error unless org is safe to embed in a managed
// folder name and in a workload identity principal set identifier.
func ValidateOrgName(org string) error {
	if !validOrgName.MatchString(org) {
		return fmt.Errorf("%w: %q must match %s", ErrInvalidOrgName, org, validOrgName.String())
	}
	return nil
}

// ManagedFoldersClient is a simplified interface to the GCS managed folders API.
type ManagedFoldersClient interface {
	GetManagedFolder(ctx context.Context, bucket, folder string) (*storage.ManagedFolder, error)
	InsertManagedFolder(ctx context.Context, bucket string, folder *storage.ManagedFolder) (*storage.ManagedFolder, error)
	GetManagedFolderIamPolicy(ctx context.Context, bucket, folder string) (*storage.Policy, error)
	SetManagedFolderIamPolicy(ctx context.Context, bucket, folder string, policy *storage.Policy) (*storage.Policy, error)
}

// FolderManager ensures each org has a managed folder in the archive bucket
// that only the org's federated identities can write to.
type FolderManager struct {
	client        ManagedFoldersClient
	Project       string
	ProjectNumber string
	Pool          string
}

// NewFolderManager creates a FolderManager for the given project. The project
// number and workload identity pool ID identify the pool whose org-scoped
// principal sets are granted write access.
func NewFolderManager(client ManagedFoldersClient, project, projectNumber, pool string) (*FolderManager, error) {
	if project == "" {
		return nil, errors.New("project must not be empty")
	}
	if !validProjectNumber.MatchString(projectNumber) {
		return nil, fmt.Errorf("invalid project number %q: must be decimal digits", projectNumber)
	}
	if !validPoolID.MatchString(pool) {
		return nil, fmt.Errorf("invalid workload identity pool ID %q: must match %s", pool, validPoolID.String())
	}
	return &FolderManager{
		client:        client,
		Project:       project,
		ProjectNumber: projectNumber,
		Pool:          pool,
	}, nil
}

// Bucket returns the bucket that holds org upload folders, e.g. archive-mlab-foo.
func (f *FolderManager) Bucket() string {
	return "archive-" + f.Project
}

// folderName returns the managed folder name for org, e.g. autoload/v3/org=foo/.
// The trailing slash keeps one org's folder from being a prefix of another's.
func (f *FolderManager) folderName(org string) (string, error) {
	if err := ValidateOrgName(org); err != nil {
		return "", err
	}
	return "autoload/v3/org=" + org + "/", nil
}

// orgMember returns the IAM principal set of all federated identities whose
// "org" attribute equals org.
func (f *FolderManager) orgMember(org string) (string, error) {
	if err := ValidateOrgName(org); err != nil {
		return "", err
	}
	return "principalSet://iam.googleapis.com/projects/" + f.ProjectNumber +
		"/locations/global/workloadIdentityPools/" + f.Pool +
		"/attribute.org/" + org, nil
}

// EnsureOrgFolder creates the org's managed folder if it is missing and grants
// the org's principal set permission to create objects in it. Existing policy
// bindings are preserved, and the policy is not written when the binding is
// already present.
func (f *FolderManager) EnsureOrgFolder(ctx context.Context, org string) error {
	folder, err := f.folderName(org)
	if err != nil {
		return err
	}
	member, err := f.orgMember(org)
	if err != nil {
		return err
	}
	bucket := f.Bucket()

	if err := f.ensureFolder(ctx, bucket, folder); err != nil {
		return err
	}

	curr, err := f.client.GetManagedFolderIamPolicy(ctx, bucket, folder)
	if err != nil {
		log.Printf("failed to get IAM policy for gs://%s/%s: %v", bucket, folder, err)
		return err
	}
	if curr == nil {
		return fmt.Errorf("nil IAM policy returned for gs://%s/%s", bucket, folder)
	}
	bindings, changed := addUnconditionalMember(curr.Bindings, orgUploadRole, member)
	if !changed {
		log.Printf("binding already present on gs://%s/%s: %s %s", bucket, folder, orgUploadRole, member)
		return nil
	}
	// Read-modify-write: send back the etag so a concurrent change fails
	// instead of being overwritten.
	next := &storage.Policy{
		Bindings: bindings,
		Etag:     curr.Etag,
		Version:  curr.Version,
	}
	_, err = f.client.SetManagedFolderIamPolicy(ctx, bucket, folder, next)
	if err != nil {
		log.Printf("failed to set IAM policy for gs://%s/%s: %v", bucket, folder, err)
		return err
	}
	log.Printf("granted %s on gs://%s/%s to %s", orgUploadRole, bucket, folder, member)
	return nil
}

func (f *FolderManager) ensureFolder(ctx context.Context, bucket, folder string) error {
	_, err := f.client.GetManagedFolder(ctx, bucket, folder)
	if err == nil {
		log.Printf("managed folder exists: gs://%s/%s", bucket, folder)
		return nil
	}
	if !isHTTPStatus(err, http.StatusNotFound) {
		log.Printf("failed to get managed folder gs://%s/%s: %v", bucket, folder, err)
		return err
	}
	_, err = f.client.InsertManagedFolder(ctx, bucket, &storage.ManagedFolder{Name: folder})
	if isHTTPStatus(err, http.StatusConflict) {
		// Created concurrently; the policy step below still applies.
		log.Printf("managed folder already exists: gs://%s/%s", bucket, folder)
		return nil
	}
	if err != nil {
		log.Printf("failed to create managed folder gs://%s/%s: %v", bucket, folder, err)
		return err
	}
	log.Printf("created managed folder gs://%s/%s", bucket, folder)
	return nil
}

// addUnconditionalMember returns bindings with member granted role through a
// binding without a condition, and whether anything changed. A conditional
// binding for the same role and member does not count, because its condition
// may restrict the grant. The input bindings are not modified.
func addUnconditionalMember(bindings []*storage.PolicyBindings, role, member string) ([]*storage.PolicyBindings, bool) {
	for _, b := range bindings {
		if b != nil && b.Condition == nil && b.Role == role && slices.Contains(b.Members, member) {
			return bindings, false
		}
	}
	result := make([]*storage.PolicyBindings, 0, len(bindings)+1)
	added := false
	for _, b := range bindings {
		if !added && b != nil && b.Condition == nil && b.Role == role {
			nb := *b
			nb.Members = append(slices.Clone(b.Members), member)
			result = append(result, &nb)
			added = true
			continue
		}
		result = append(result, b)
	}
	if !added {
		result = append(result, &storage.PolicyBindings{Role: role, Members: []string{member}})
	}
	return result, true
}

func isHTTPStatus(err error, code int) bool {
	var gerr *googleapi.Error
	return errors.As(err, &gerr) && gerr.Code == code
}

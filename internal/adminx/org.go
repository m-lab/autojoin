package adminx

import (
	"context"
	"fmt"
	"log"

	"github.com/m-lab/autojoin/internal/dnsname"
	"golang.org/x/exp/slices"

	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/iam/v1"
)

var (
	// Restrict uploads to the organization prefix. Needed to share bucket write access.
	expUploadFmt = (`resource.name.startsWith("projects/_/buckets/archive-%s/objects/autoload/v2/%s") ||` +
		` resource.name.startsWith("projects/_/buckets/staging-%s/objects/autoload/v2/%s")`)
	// Restrict reads to the archive bucket. Needed so nodes can read jostler schemas.
	expReadFmt = (`resource.name.startsWith("projects/_/buckets/archive-%s") ||` +
		` resource.name.startsWith("projects/_/buckets/downloader-%s") ||` +
		` resource.name.startsWith("projects/_/buckets/staging-%s")`)

	// Allow uploads to include tables. Needed for the authoritative schema update path.
	expUploadTablesFmt = (`resource.name.startsWith("projects/_/buckets/archive-%s/objects/autoload/v2/%s") ||` +
		` resource.name.startsWith("projects/_/buckets/staging-%s/objects/autoload/v2/%s") ||` +
		` resource.name.startsWith("projects/_/buckets/archive-%s/objects/autoload/v2/tables") ||` +
		` resource.name.startsWith("projects/_/buckets/staging-%s/objects/autoload/v2/tables")`)
)

// DNS is a simplified interface to the Google Cloud DNS API.
type DNS interface {
	RegisterZone(ctx context.Context, zone *dns.ManagedZone) (*dns.ManagedZone, error)
	RegisterZoneSplit(ctx context.Context, zone *dns.ManagedZone) (*dns.ResourceRecordSet, error)
	DeleteZoneSplit(ctx context.Context, zone *dns.ManagedZone) error
	DeleteZone(ctx context.Context, zoneName string) error
}

// CRM is a simplified interface to the Google Cloud Resource Manager API.
type CRM interface {
	GetIamPolicy(ctx context.Context, req *cloudresourcemanager.GetIamPolicyRequest) (*cloudresourcemanager.Policy, error)
	SetIamPolicy(ctx context.Context, req *cloudresourcemanager.SetIamPolicyRequest) error
}

// OrganizationManager defines the interface for managing organizations and their API keys
type OrganizationManager interface {
	CreateOrganization(ctx context.Context, name, email string) error
	CreateAPIKeyWithValue(ctx context.Context, org, value string) (string, error)
	GetAPIKeys(ctx context.Context, org string) ([]string, error)
	DeleteAPIKeys(ctx context.Context, org string) error
	DeleteOrganization(ctx context.Context, org string) error
}

// Org contains fields needed to setup a new organization for Autojoined nodes.
type Org struct {
	Project      string
	crm          CRM
	sam          *ServiceAccountsManager
	sm           *SecretManager
	orgm         OrganizationManager
	dns          DNS
	updateTables bool
}

// NewOrg creates a new Org instance for setting up a new organization.
func NewOrg(project string, crm CRM, sam *ServiceAccountsManager, sm *SecretManager, dns DNS,
	orgm OrganizationManager, updateTables bool) *Org {
	return &Org{
		Project:      project,
		crm:          crm,
		sam:          sam,
		sm:           sm,
		orgm:         orgm,
		dns:          dns,
		updateTables: updateTables,
	}
}

// Setup should be run once on org creation to create all Google Cloud resources needed by the Autojoin API.
func (o *Org) Setup(ctx context.Context, org string, email string) error {
	// Create organization in Datastore
	err := o.orgm.CreateOrganization(ctx, org, email)
	if err != nil {
		return err
	}
	// Create service account with no keys.
	sa, err := o.sam.CreateServiceAccount(ctx, org)
	if err != nil {
		return err
	}
	err = o.ApplyPolicy(ctx, org, sa, o.updateTables)
	if err != nil {
		return err
	}
	// Create secret with no versions.
	err = o.sm.CreateSecret(ctx, org)
	if err != nil {
		return err
	}
	// Create DNS zone and zone split.
	err = o.RegisterDNS(ctx, org)
	if err != nil {
		return err
	}
	return nil
}

// Delete removes all setup-created resources for org.
func (o *Org) Delete(ctx context.Context, org string) error {
	if err := o.orgm.DeleteAPIKeys(ctx, org); err != nil {
		return err
	}

	zone := &dns.ManagedZone{
		Name:    dnsname.OrgZone(org, o.Project),
		DnsName: dnsname.OrgDNS(org, o.Project),
	}
	if err := o.dns.DeleteZoneSplit(ctx, zone); err != nil {
		return err
	}
	if err := o.dns.DeleteZone(ctx, zone.Name); err != nil {
		return err
	}
	if err := o.sm.DeleteSecret(ctx, org); err != nil {
		return err
	}
	if err := o.RemovePolicy(ctx, org); err != nil {
		return err
	}
	if err := o.sam.DeleteServiceAccount(ctx, org); err != nil {
		return err
	}
	return o.orgm.DeleteOrganization(ctx, org)
}

// RegisterDNS creates the organization zone and the zone split within the project zone.
func (o *Org) RegisterDNS(ctx context.Context, org string) error {
	zone, err := o.dns.RegisterZone(ctx, &dns.ManagedZone{
		Description: "Autojoin registered nodes from org: " + org,
		Name:        dnsname.OrgZone(org, o.Project),
		DnsName:     dnsname.OrgDNS(org, o.Project),
		DnssecConfig: &dns.ManagedZoneDnsSecConfig{
			State: "on",
		},
	})
	if err != nil {
		log.Println("failed to register zone:", dnsname.OrgZone(org, o.Project), err)
		return err
	}
	_, err = o.dns.RegisterZoneSplit(ctx, zone)
	if err != nil {
		log.Println("failed to register zone split:", dnsname.OrgZone(org, o.Project), err)
		return err
	}
	return nil
}

// ApplyPolicy adds write restrictions for shared GCS buckets.
// NOTE: By operating on project IAM policies, this method modifies project wide state.
func (o *Org) ApplyPolicy(ctx context.Context, org string, account *iam.ServiceAccount, updateTables bool) error {
	// Get current policy.
	req := &cloudresourcemanager.GetIamPolicyRequest{
		Options: &cloudresourcemanager.GetPolicyOptions{
			RequestedPolicyVersion: 3,
		},
	}
	curr, err := o.crm.GetIamPolicy(ctx, req)
	if err != nil {
		log.Println("get policy", err)
		return err
	}
	// Setup new bindings.
	bindings := []*cloudresourcemanager.Binding{
		o.uploadBinding(org, account.Email, updateTables),
		o.readBinding(org, account.Email),
	}

	// Append the new bindings if missing from the current set.
	newBindings, wasMissing := appendBindingIfMissing(curr.Bindings, bindings...)

	// Apply bindings if any were missing.
	// Version 3 is required for policies with conditional role bindings.
	preq := &cloudresourcemanager.SetIamPolicyRequest{
		Policy: &cloudresourcemanager.Policy{
			AuditConfigs: curr.AuditConfigs,
			Bindings:     newBindings,
			Etag:         curr.Etag,
			Version:      3,
		},
	}

	if wasMissing {
		err = o.crm.SetIamPolicy(ctx, preq)
		if err != nil {
			log.Println("set policy", err)
			return err
		}
	}
	return nil
}

// RemovePolicy removes org-specific conditional IAM bindings.
func (o *Org) RemovePolicy(ctx context.Context, org string) error {
	req := &cloudresourcemanager.GetIamPolicyRequest{
		Options: &cloudresourcemanager.GetPolicyOptions{
			RequestedPolicyVersion: 3,
		},
	}
	curr, err := o.crm.GetIamPolicy(ctx, req)
	if err != nil {
		log.Println("get policy", err)
		return err
	}

	email := o.sam.Namer.GetServiceAccountEmail(org)
	targets := []*cloudresourcemanager.Binding{
		o.readBinding(org, email),
		o.uploadBinding(org, email, false),
		o.uploadBinding(org, email, true),
	}
	newBindings, removed := removeBindings(curr.Bindings, targets...)
	if !removed {
		return nil
	}

	preq := &cloudresourcemanager.SetIamPolicyRequest{
		Policy: &cloudresourcemanager.Policy{
			AuditConfigs: curr.AuditConfigs,
			Bindings:     newBindings,
			Etag:         curr.Etag,
			Version:      3,
		},
	}
	err = o.crm.SetIamPolicy(ctx, preq)
	if err != nil {
		log.Println("set policy", err)
		return err
	}
	return nil
}

func (o *Org) uploadBinding(org, email string, updateTables bool) *cloudresourcemanager.Binding {
	expression := ""
	role := ""
	if updateTables {
		expression = fmt.Sprintf(expUploadTablesFmt, o.Project, org, o.Project, org, o.Project, o.Project)
		role = "roles/storage.objectUser"
	} else {
		expression = fmt.Sprintf(expUploadFmt, o.Project, org, o.Project, org)
		role = "roles/storage.objectCreator"
	}
	return &cloudresourcemanager.Binding{
		Condition: &cloudresourcemanager.Expr{
			Title:      "Upload restriction for " + org,
			Expression: expression,
		},
		Members: []string{"serviceAccount:" + email},
		Role:    role,
	}
}

func (o *Org) readBinding(org, email string) *cloudresourcemanager.Binding {
	return &cloudresourcemanager.Binding{
		Condition: &cloudresourcemanager.Expr{
			Title:      "Read restriction for " + org,
			Expression: fmt.Sprintf(expReadFmt, o.Project, o.Project, o.Project),
		},
		Members: []string{"serviceAccount:" + email},
		Role:    "roles/storage.objectViewer",
	}
}

func appendBindingIfMissing(slice []*cloudresourcemanager.Binding, elems ...*cloudresourcemanager.Binding) ([]*cloudresourcemanager.Binding, bool) {
	result := []*cloudresourcemanager.Binding{}
	foundMissing := false
	for _, b := range elems {
		found := false
		// Does slice contain B?
		for _, a := range slice {
			if BindingIsEqual(a, b) {
				// We found a matching binding.
				found = true
				break
			}
		}
		if !found {
			// slice does not contain B, so add it to results.
			result = append(result, b)
			foundMissing = true
		}
	}
	// Return all bindings
	return append(result, slice...), foundMissing
}

func removeBindings(slice []*cloudresourcemanager.Binding, elems ...*cloudresourcemanager.Binding) ([]*cloudresourcemanager.Binding, bool) {
	result := []*cloudresourcemanager.Binding{}
	removed := false
	for _, current := range slice {
		matched := false
		for _, target := range elems {
			if BindingIsEqual(current, target) {
				matched = true
				removed = true
				break
			}
		}
		if !matched {
			result = append(result, current)
		}
	}
	return result, removed
}

// BindingIsEqual checks wether the two provided bindings contain equal conditions, members, and roles.
func BindingIsEqual(a *cloudresourcemanager.Binding, b *cloudresourcemanager.Binding) bool {
	if (a.Condition != nil) != (b.Condition != nil) {
		// Either both should have conditions or neither.
		return false
	}
	if a.Condition != nil {
		// We established above that both are non-nil.
		if a.Condition.Expression != b.Condition.Expression {
			// Expressions should match.
			return false
		}
	}
	// Check membership in both directions: are all members of a in b, and b in a?
	for i := range a.Members {
		if !slices.Contains(b.Members, a.Members[i]) {
			// Each member in A should be found in B.
			return false
		}
	}
	for i := range b.Members {
		if !slices.Contains(a.Members, b.Members[i]) {
			// Each member in B should be found in A.
			return false
		}
	}
	// Roles should match.
	return a.Role == b.Role
}

func (o *Org) CreateAPIKeyWithValue(ctx context.Context, org, val string) (string, error) {
	return o.orgm.CreateAPIKeyWithValue(ctx, org, val)
}

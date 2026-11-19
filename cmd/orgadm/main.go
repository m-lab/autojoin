// orgadm creates the Google Cloud resources for an Autojoin organization.
//
// A new org gets a Datastore entity, a GCS managed folder autoload/v2/<org>/
// in archive-<project> writable only by the org's federated identities, a DNS
// zone, and an API key. Each of these is created only if it is missing, so
// running orgadm again for the same org changes nothing that already exists.
// With -storage-only, orgadm only ensures the managed folder and its binding
// for an org that already exists in Datastore.
package main

import (
	"context"
	"flag"
	"log"
	"strconv"

	"cloud.google.com/go/datastore"
	"github.com/m-lab/autojoin/internal/adminx"
	"github.com/m-lab/autojoin/internal/adminx/storageiface"
	"github.com/m-lab/autojoin/internal/dnsname"
	"github.com/m-lab/autojoin/internal/dnsx"
	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"github.com/m-lab/go/rtx"
	"github.com/m-lab/token-exchange/store"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/storage/v1"
)

var (
	org                string
	orgEmail           string
	project            string
	credentialsProject string
	storageOnly        bool
)

func init() {
	flag.StringVar(&org, "org", "", "Organization name. Must match name assigned by M-Lab")
	flag.StringVar(&project, "project", "mlab-autojoin", "GCP project to create organization resources")
	flag.StringVar(&credentialsProject, "credentials-project", "mlab-oti", "GCP project for credentials Datastore")
	flag.StringVar(&orgEmail, "org-email", "", "Organization contact email")
	flag.BoolVar(&storageOnly, "storage-only", false, "Only ensure the org's managed folder and binding; the org must already exist in Datastore")
}

func main() {
	flag.Parse()
	log.SetFlags(log.Lshortfile | log.LUTC)

	if org == "" {
		log.Fatalf("-org is a required flag")
	}
	rtx.Must(adminx.ValidateOrgName(org), "invalid -org")

	ctx := context.Background()
	// Workload identity principals are named by project number, not ID.
	crm, err := cloudresourcemanager.NewService(ctx)
	rtx.Must(err, "failed to create resource manager client")
	p, err := crm.Projects.Get(project).Context(ctx).Do()
	rtx.Must(err, "failed to look up project number of "+project)
	projectNumber := strconv.FormatInt(p.ProjectNumber, 10)
	sc, err := storage.NewService(ctx)
	rtx.Must(err, "failed to create storage service client")
	fm, err := adminx.NewFolderManager(storageiface.NewStorage(sc), project, projectNumber, adminx.WorkloadIdentityPoolID)
	rtx.Must(err, "failed to create folder manager")

	// Setup Datastore client for credentials (may be in a different project).
	dsc, err := datastore.NewClient(ctx, credentialsProject)
	rtx.Must(err, "failed to create datastore client")
	defer dsc.Close()

	// Initialize AutojoinManager from token-exchange with the correct namespace.
	am := store.NewAutojoinManager(dsc, credentialsProject, "platform-credentials")

	if storageOnly {
		// Does not call CreateOrganization, which would overwrite the existing entity.
		o := adminx.NewOrg(project, fm, nil, am)
		err = o.SetupStorage(ctx, org)
		rtx.Must(err, "failed to set up storage for organization: "+org)
		log.Println("Storage setup okay - org:", org)
		return
	}

	dnsService, err := dns.NewService(ctx)
	rtx.Must(err, "failed to create new dns service")
	d := dnsx.NewManager(dnsiface.NewCloudDNSService(dnsService), project, dnsname.ProjectZone(project))

	o := adminx.NewOrg(project, fm, d, am)
	err = o.Setup(ctx, org, orgEmail)
	rtx.Must(err, "failed to set up new organization: "+org)

	// Generate and store an API key for autojoin/heartbeat authentication,
	// unless the org already has one.
	keys, err := am.GetAPIKeys(ctx, org)
	rtx.Must(err, "failed to list API keys")
	if len(keys) > 0 {
		log.Println("Setup okay - org:", org)
		log.Printf("API_KEY: not created, the org already has %d", len(keys))
		return
	}

	apiKey, err := store.GenerateAPIKey()
	rtx.Must(err, "failed to generate API key")

	_, err = am.CreateAPIKeyWithValue(ctx, org, apiKey)
	rtx.Must(err, "failed to create API key")

	log.Println("Setup okay - org:", org)
	log.Println("API_KEY:", apiKey)
}

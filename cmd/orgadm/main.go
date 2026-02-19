package main

import (
	"context"
	"flag"
	"log"

	"cloud.google.com/go/datastore"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"github.com/m-lab/autojoin/internal/adminx"
	"github.com/m-lab/autojoin/internal/adminx/crmiface"
	"github.com/m-lab/autojoin/internal/adminx/iamiface"
	"github.com/m-lab/autojoin/internal/dnsname"
	"github.com/m-lab/autojoin/internal/dnsx"
	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"github.com/m-lab/go/rtx"
	"github.com/m-lab/token-exchange/store"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/dns/v1"
	iam "google.golang.org/api/iam/v1"
)

var (
	org                string
	orgEmail           string
	project            string
	credentialsProject string
	updateTables       bool
)

func init() {
	flag.StringVar(&org, "org", "", "Organization name. Must match name assigned by M-Lab")
	flag.StringVar(&project, "project", "mlab-autojoin", "GCP project to create organization resources")
	flag.StringVar(&credentialsProject, "credentials-project", "mlab-oti", "GCP project for credentials Datastore")
	flag.BoolVar(&updateTables, "update-tables", false, "Allow this org's service account to update table schemas")
	flag.StringVar(&orgEmail, "org-email", "", "Organization contact email")
}

func main() {
	flag.Parse()
	log.SetFlags(log.Lshortfile | log.LUTC)

	if org == "" {
		log.Fatalf("-org is a required flag")
	}

	ctx := context.Background()
	sc, err := secretmanager.NewClient(ctx)
	rtx.Must(err, "failed to create secretmanager client")
	defer sc.Close()
	ic, err := iam.NewService(ctx)
	rtx.Must(err, "failed to create iam service client")
	nn := adminx.NewNamer(project)
	crm, err := cloudresourcemanager.NewService(ctx)
	rtx.Must(err, "failed to allocate new cloud resource manager client")
	sa := adminx.NewServiceAccountsManager(iamiface.NewIAM(ic), nn)
	sm := adminx.NewSecretManager(sc, nn, sa)
	dnsService, err := dns.NewService(ctx)
	rtx.Must(err, "failed to create new dns service")
	d := dnsx.NewManager(dnsiface.NewCloudDNSService(dnsService), project, dnsname.ProjectZone(project))

	// Setup Datastore client for credentials (may be in a different project).
	dsc, err := datastore.NewClient(ctx, credentialsProject)
	rtx.Must(err, "failed to create datastore client")
	defer dsc.Close()

	// Initialize AutojoinManager from token-exchange with the correct namespace.
	am := store.NewAutojoinManager(dsc, credentialsProject, "platform-credentials")

	o := adminx.NewOrg(project, crmiface.NewCRM(project, crm), sa, sm, d, am, updateTables)
	err = o.Setup(ctx, org, orgEmail)
	rtx.Must(err, "failed to set up new organization: "+org)

	// Generate and store API key for autojoin/heartbeat authentication.
	apiKey, err := store.GenerateAPIKey()
	rtx.Must(err, "failed to generate API key")

	_, err = am.CreateAPIKeyWithValue(ctx, org, apiKey)
	rtx.Must(err, "failed to create API key")

	log.Println("Setup okay - org:", org)
	log.Println("API_KEY:", apiKey)
}

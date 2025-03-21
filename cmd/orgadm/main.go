package main

import (
	"context"
	"flag"
	"log"

	apikeys "cloud.google.com/go/apikeys/apiv2"
	"cloud.google.com/go/datastore"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"github.com/m-lab/autojoin/internal/adminx"
	"github.com/m-lab/autojoin/internal/adminx/crmiface"
	"github.com/m-lab/autojoin/internal/adminx/iamiface"
	"github.com/m-lab/autojoin/internal/adminx/keysiface"
	"github.com/m-lab/autojoin/internal/dnsname"
	"github.com/m-lab/autojoin/internal/dnsx"
	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"github.com/m-lab/go/rtx"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/dns/v1"
	iam "google.golang.org/api/iam/v1"
)

var (
	org           string
	orgEmail      string
	project       string
	locateProject string
	updateTables  bool
)

func init() {
	flag.StringVar(&org, "org", "", "Organization name. Must match name assigned by M-Lab")
	flag.StringVar(&project, "project", "", "GCP project to create organization resources")
	flag.StringVar(&locateProject, "locate-project", "", "GCP project for Locate API")
	flag.BoolVar(&updateTables, "update-tables", false, "Allow this org's service account to update table schemas")
	flag.StringVar(&orgEmail, "org-email", "", "Organization contact email")
}

func main() {
	flag.Parse()
	log.SetFlags(log.Lshortfile | log.LUTC)

	if org == "" || project == "" {
		log.Fatalf("-org and -project are required flags")
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
	rtx.Must(err, "failed to create sam")
	sm := adminx.NewSecretManager(sc, nn, sa)
	dnsService, err := dns.NewService(ctx)
	rtx.Must(err, "failed to create new dns service")
	d := dnsx.NewManager(dnsiface.NewCloudDNSService(dnsService), project, dnsname.ProjectZone(project))

	// Create Datastore client
	dsc, err := datastore.NewClient(ctx, project)
	rtx.Must(err, "failed to create datastore client")
	defer dsc.Close()

	// Initialize Datastore manager
	ds := adminx.NewDatastoreManager(dsc, project)

	ac, err := apikeys.NewClient(ctx)
	rtx.Must(err, "failed to create new apikey client")
	if project == "mlab-autojoin" && locateProject == "" {
		locateProject = "mlab-ns"
	}

	// Local project names are taken from the namer.
	k := adminx.NewAPIKeys(locateProject, keysiface.NewKeys(ac), nn)
	defer ac.Close()

	o := adminx.NewOrg(project, crmiface.NewCRM(project, crm), sa, sm, d, k, ds, updateTables)
	err = o.Setup(ctx, org, orgEmail)
	rtx.Must(err, "failed to set up new organization: "+org)

	// Create API key for Locate/Heartbeat.
	locateKey, err := k.CreateKey(ctx, org)
	rtx.Must(err, "failed to create locate key")

	// Create API key for autojoin with the same value as the Locate key.
	key, err := o.CreateAPIKeyWithValue(ctx, org, locateKey)

	log.Println("Setup okay - org:", org)
	log.Println("API_KEY:", key)
}

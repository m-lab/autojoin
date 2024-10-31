package main

import (
	"context"
	"flag"
	"log"

	apikeys "cloud.google.com/go/apikeys/apiv2"
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
	org          string
	project      string
	updateTables bool
)

func init() {
	flag.StringVar(&org, "org", "", "Organization name. Must match name assigned by M-Lab")
	flag.StringVar(&project, "project", "", "GCP project to create organization resources")
	flag.BoolVar(&updateTables, "update-tables", false, "Allow this org's service account to update table schemas")
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
	ds, err := dns.NewService(ctx)
	rtx.Must(err, "failed to create new dns service")
	d := dnsx.NewManager(dnsiface.NewCloudDNSService(ds), project, dnsname.ProjectZone(project))
	ac, err := apikeys.NewClient(ctx)
	rtx.Must(err, "failed to create new apikey client")
	k := adminx.NewAPIKeys(project, keysiface.NewKeys(ac), nn)
	defer ac.Close()

	o := adminx.NewOrg(project, crmiface.NewCRM(project, crm), sa, sm, d, k, updateTables)
	key, err := o.Setup(ctx, org)
	rtx.Must(err, "failed to set up new organization: "+org)
	log.Println("Setup okay - org:", org, "key:", key)
}

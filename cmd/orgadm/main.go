package main

import (
	"context"
	"flag"
	"log"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"github.com/m-lab/autojoin/internal/adminx"
	"github.com/m-lab/autojoin/internal/adminx/crmiface"
	"github.com/m-lab/autojoin/internal/adminx/iamiface"
	"github.com/m-lab/go/rtx"
	"google.golang.org/api/cloudresourcemanager/v1"
	iam "google.golang.org/api/iam/v1"
)

var (
	org     string
	project string
)

func init() {
	flag.StringVar(&org, "org", "", "Organization name. Must match name assigned by M-Lab")
	flag.StringVar(&project, "project", "", "GCP project to create organization resources")
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
	log.Println("Creating SAM & KEYS")
	nn := adminx.NewNamer(project)
	crm, err := cloudresourcemanager.NewService(ctx)
	rtx.Must(err, "failed to allocate new cloud resource manager client")
	sa := adminx.NewServiceAccountsManager(iamiface.NewIAM(ic), nn)
	rtx.Must(err, "failed to create sam")
	sm := adminx.NewSecretManager(sc, nn, sa)
	o := adminx.NewOrg(project, crmiface.NewCRM(project, crm), sa, sm)
	o.Setup(ctx, org)
}

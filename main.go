package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/datastore"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"github.com/gomodule/redigo/redis"
	"github.com/m-lab/autojoin/handler"
	"github.com/m-lab/autojoin/iata"
	"github.com/m-lab/autojoin/internal/adminx"
	"github.com/m-lab/autojoin/internal/adminx/iamiface"
	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"github.com/m-lab/autojoin/internal/maxmind"
	"github.com/m-lab/autojoin/internal/metrics"
	"github.com/m-lab/autojoin/internal/tracker"
	"github.com/m-lab/go/content"
	"github.com/m-lab/go/flagx"
	"github.com/m-lab/go/httpx"
	"github.com/m-lab/go/memoryless"
	"github.com/m-lab/go/prometheusx"
	"github.com/m-lab/go/rtx"
	"github.com/m-lab/locate/memorystore"
	"github.com/m-lab/uuid-annotator/asnannotator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/iam/v1"
)

var (
	listenPort   string
	project      string
	redisAddr    string
	minVersion   string
	iataSrc      = flagx.MustNewURL("https://raw.githubusercontent.com/ip2location/ip2location-iata-icao/1.0.25/iata-icao.csv")
	maxmindSrc   = flagx.URL{}
	routeviewSrc = flagx.URL{}
	gcTTL        time.Duration
	gcInterval   time.Duration
)

func init() {
	// PORT and GOOGLE_CLOUD_PROJECT are part of the default App Engine environment.
	flag.StringVar(&listenPort, "port", "8080", "AppEngine port environment variable")
	flag.StringVar(&project, "google-cloud-project", "", "AppEngine project environment variable")
	flag.StringVar(&minVersion, "min-version", "0.0.0", "Minimum version of the client to accept")
	flag.Var(&iataSrc, "iata-url", "URL to IATA dataset")
	flag.Var(&maxmindSrc, "maxmind-url", "URL of a Maxmind GeoIP dataset, e.g. gs://bucket/file or file:./relativepath/file")
	flag.Var(&routeviewSrc, "routeview-v4.url", "URL of an ip2prefix routeview IPv4 dataset, e.g. gs://bucket/file and file:./relativepath/file")
	flag.StringVar(&redisAddr, "redis-address", "", "Primary endpoint for Redis instance")

	flag.DurationVar(&gcTTL, "gc-ttl", 3*time.Hour, "Time to live for DNS entries")
	flag.DurationVar(&gcInterval, "gc-interval", 30*time.Minute, "Interval between garbage collection runs")

	// Enable logging with line numbers to trace error locations.
	log.SetFlags(log.LUTC | log.Llongfile)
}

var mainCtx, mainCancel = context.WithCancel(context.Background())

func main() {
	flag.Parse()
	rtx.Must(flagx.ArgsFromEnv(flag.CommandLine), "Could not parse env args")
	defer mainCancel()

	prom := prometheusx.MustServeMetrics()
	defer prom.Close()

	// Setup DNS service.
	dnsService, err := dns.NewService(mainCtx)
	rtx.Must(err, "failed to create new dns service")
	d := dnsiface.NewCloudDNSService(dnsService)

	// Setup IATA, maxmind, and asn sources.
	i, err := iata.New(mainCtx, iataSrc.URL)
	rtx.Must(err, "failed to load iata dataset")
	mmsrc, err := content.FromURL(mainCtx, maxmindSrc.URL)
	rtx.Must(err, "failed to load maxmindurl: %s", maxmindSrc.URL)
	mm := maxmind.NewMaxmind(mmsrc)
	rvsrc, err := content.FromURL(mainCtx, routeviewSrc.URL)
	rtx.Must(err, "Could not load routeview v4 URL")
	asn := asnannotator.NewIPv4(mainCtx, rvsrc)

	// Secret Manager & Service Accounts
	sc, err := secretmanager.NewClient(mainCtx)
	rtx.Must(err, "failed to create secretmanager client")
	defer sc.Close()
	ic, err := iam.NewService(mainCtx)
	rtx.Must(err, "failed to create iam service client")
	n := adminx.NewNamer(project)
	sa := adminx.NewServiceAccountsManager(iamiface.NewIAM(ic), n)
	sm := adminx.NewSecretManager(sc, n, sa)

	// Connect to memorystore.
	pool := &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp", redisAddr)
		},
	}
	msClient := memorystore.NewClient[tracker.Status](pool)

	// Test connection by calling GetAll
	entries, err := msClient.GetAll()
	rtx.Must(err, "Could not connect to memorystore")
	log.Printf("Connected to memorystore at %s", redisAddr)
	log.Printf("Number of tracked DNS entries: %d", len(entries))

	gc := tracker.NewGarbageCollector(d, project, msClient, gcTTL, gcInterval)
	log.Print("DNS garbage collector started")
	defer gc.Stop()

	// Setup Datastore client
	ds, err := datastore.NewClient(mainCtx, project)
	rtx.Must(err, "failed to create datastore client")
	defer ds.Close()
	dsm := adminx.NewDatastoreManager(ds, project)

	// Create server.
	s := handler.NewServer(project, i, mm, asn, d, gc, sm, dsm, minVersion)
	go func() {
		// Load once.
		s.Iata.Load(mainCtx)
		s.Maxmind.Reload(mainCtx)
		s.ASN.Reload(mainCtx)

		// Check and reload db at least once a day.
		reloadConfig := memoryless.Config{
			Min:      12 * time.Hour,
			Max:      3 * 24 * time.Hour,
			Expected: 24 * time.Hour,
		}
		tick, err := memoryless.NewTicker(mainCtx, reloadConfig)
		rtx.Must(err, "Could not create ticker for reloading")
		for range tick.C {
			s.Iata.Load(mainCtx)
			s.Maxmind.Reload(mainCtx)
			s.ASN.Reload(mainCtx)
		}
	}()

	mux := http.NewServeMux()
	// USER APIs
	mux.HandleFunc("/autojoin/v0/lookup", promhttp.InstrumentHandlerDuration(
		metrics.RequestHandlerDuration.MustCurryWith(prometheus.Labels{"path": "/autojoin/v0/lookup"}),
		http.HandlerFunc(s.Lookup)))

	// AUTOJOIN APIs
	// Nodes register on start up.
	mux.HandleFunc("/autojoin/v0/node/register", promhttp.InstrumentHandlerDuration(
		metrics.RequestHandlerDuration.MustCurryWith(prometheus.Labels{"path": "/autojoin/v0/node/register"}),
		handler.WithAPIKeyValidation(dsm, s.Register)))

	mux.HandleFunc("/autojoin/v0/node/delete", promhttp.InstrumentHandlerDuration(
		metrics.RequestHandlerDuration.MustCurryWith(prometheus.Labels{"path": "/autojoin/v0/node/delete"}),
		handler.WithAPIKeyValidation(dsm, s.Delete)))

	mux.HandleFunc("/autojoin/v0/node/list", promhttp.InstrumentHandlerDuration(
		metrics.RequestHandlerDuration.MustCurryWith(prometheus.Labels{"path": "/autojoin/v0/node/list"}),
		http.HandlerFunc(s.List)))

	// Liveness and Readiness checks to support deployments.
	mux.HandleFunc("/v0/live", s.Live)
	mux.HandleFunc("/v0/ready", s.Ready)

	srv := &http.Server{
		Addr:    ":" + listenPort,
		Handler: mux,
	}
	log.Println("Listening for INSECURE access requests on " + listenPort)
	rtx.Must(httpx.ListenAndServeAsync(srv), "Could not start server")
	defer srv.Close()
	<-mainCtx.Done()
}

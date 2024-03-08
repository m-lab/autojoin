package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/m-lab/autojoin/handler"
	"github.com/m-lab/autojoin/iata"
	"github.com/m-lab/autojoin/internal/dnsx/dnsiface"
	"github.com/m-lab/autojoin/internal/maxmind"
	"github.com/m-lab/go/content"
	"github.com/m-lab/go/flagx"
	"github.com/m-lab/go/httpx"
	"github.com/m-lab/go/memoryless"
	"github.com/m-lab/go/prometheusx"
	"github.com/m-lab/go/rtx"
	"github.com/m-lab/uuid-annotator/asnannotator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/api/dns/v1"
)

var (
	listenPort   string
	project      string
	iataSrc      = flagx.MustNewURL("https://raw.githubusercontent.com/ip2location/ip2location-iata-icao/1.0.10/iata-icao.csv")
	maxmindSrc   = flagx.URL{}
	routeviewSrc = flagx.URL{}

	// RequestHandlerDuration is a histogram that tracks the latency of each request handler.
	RequestHandlerDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "autojoin_request_handler_duration",
			Help: "A histogram of latencies for each request handler.",
		},
		[]string{"path", "code"},
	)
)

func init() {
	// PORT and GOOGLE_CLOUD_PROJECT are part of the default App Engine environment.
	flag.StringVar(&listenPort, "port", "8080", "AppEngine port environment variable")
	flag.StringVar(&project, "google-cloud-project", "", "AppEngine project environment variable")
	flag.Var(&iataSrc, "iata-url", "URL to IATA dataset")
	flag.Var(&maxmindSrc, "maxmind-url", "URL of a Maxmind GeoIP dataset, e.g. gs://bucket/file or file:./relativepath/file")
	flag.Var(&routeviewSrc, "routeview-v4.url", "URL of an ip2prefix routeview IPv4 dataset, e.g. gs://bucket/file and file:./relativepath/file")

	// Enable logging with line numbers to trace error locations.
	log.SetFlags(log.LUTC | log.Llongfile)
}

var mainCtx, mainCancel = context.WithCancel(context.Background())

func main() {
	flag.Parse()
	rtx.Must(flagx.ArgsFromEnv(flag.CommandLine), "Could not parse env args")
	defer mainCancel()

	// Google Application Default Credentials
	ds, err := dns.NewService(mainCtx)
	rtx.Must(err, "failed to create new dns service")
	d := &dnsiface.CloudDNSService{Service: ds}

	prom := prometheusx.MustServeMetrics()
	defer prom.Close()

	i, err := iata.New(mainCtx, iataSrc.URL)
	rtx.Must(err, "failed to load iata dataset")

	mmsrc, err := content.FromURL(mainCtx, maxmindSrc.URL)
	rtx.Must(err, "failed to load maxmindurl: %s", maxmindSrc.URL)
	mm := maxmind.NewMaxmind(mmsrc)
	rvsrc, err := content.FromURL(mainCtx, routeviewSrc.URL)
	rtx.Must(err, "Could not load routeview v4 URL")
	asn := asnannotator.NewIPv4(mainCtx, rvsrc)

	s := handler.NewServer(project, i, mm, asn, d)
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
		RequestHandlerDuration.MustCurryWith(prometheus.Labels{"path": "/autojoin/v0/lookup"}),
		http.HandlerFunc(s.Lookup)))

	// AUTOJOIN APIs
	// Nodes register on start up.
	mux.HandleFunc("/autojoin/v0/node/register", promhttp.InstrumentHandlerDuration(
		RequestHandlerDuration.MustCurryWith(prometheus.Labels{"path": "/autojoin/v0/node/register"}),
		http.HandlerFunc(s.Register)))

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

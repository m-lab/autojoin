package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"github.com/m-lab/autojoin/handler"
	"github.com/m-lab/go/flagx"
	"github.com/m-lab/go/httpx"
	"github.com/m-lab/go/prometheusx"
	"github.com/m-lab/go/rtx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	listenPort         string
	project            string
	platform           string
	locatorAE          bool
	locatorMM          bool
	legacyServer       string
	signerSecretName   string
	maxmind            = flagx.URL{}
	verifySecretName   string
	redisAddr          string
	promUserSecretName string
	promPassSecretName string
	promURL            string
	limitsPath         string
	keySource          = flagx.Enum{
		Options: []string{"secretmanager", "local"},
		Value:   "secretmanager",
	}

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

	s := handler.NewServer(project)

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

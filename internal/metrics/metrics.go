package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// DNSExpiration is a metric for exposing how long until a machine's DNS
	// record will be removed from Cloud DNS.
	DNSExpiration = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "autojoin_dns_expiration",
			Help: "The amount of time before a DNS record will be removed",
		},
		[]string{
			"hostname",
		},
	)

	// RequestHandlerDuration is a histogram that tracks the latency of each request handler.
	RequestHandlerDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "autojoin_request_handler_duration",
			Help: "A histogram of latencies for each request handler.",
		},
		[]string{"path", "code"},
	)
)

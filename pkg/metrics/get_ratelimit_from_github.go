package metrics

import (
	"context"
	"log"
	"time"

	"github.com/chipgata/github-actions-exporter/pkg/config"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	rateLimitGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_ralimit_remaining_by_hour",
			Help: "Number of billable seconds used by a specific workflow during the current billing cycle. Any job re-runs are also included in the usage. Only apply to workflows in private repositories that use GitHub-hosted runners.",
		},
		[]string{},
	)
)

// getRateLimitFromGithub - return ratelimit informations.
func getRateLimitFromGithub() {
	for {
		resp, _, err := client.RateLimits(context.Background())
		if err != nil {
			log.Printf("getRateLimitFromGithub error: %s", err.Error())
			return
		}
		rateLimitGauge.WithLabelValues().Set(float64(resp.Core.Remaining))
		time.Sleep(time.Duration(config.Github.Refresh) * time.Second)
	}
}

package metrics

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/chipgata/github-actions-exporter/pkg/config"

	"github.com/coocood/freecache"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/die-net/lrucache"
	"github.com/google/go-github/v45/github"
	"github.com/gregjones/httpcache"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/oauth2"
)

var (
	cache                    *freecache.Cache
	client                   *github.Client
	err                      error
	workflowRunStatusGauge   *prometheus.GaugeVec
	workflowRunDurationGauge *prometheus.GaugeVec
)

// InitMetrics - register metrics in prometheus lib and start func for monitor
func InitMetrics() {
	cacheSize := 100 * 1024 * 1024
	cache = freecache.NewCache(cacheSize)

	workflowRunStatusGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_workflow_run_status",
			Help: "Workflow run status of all workflow runs created in the last 1hr",
		},
		strings.Split(config.WorkflowFields, ","),
	)
	workflowRunDurationGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_workflow_run_duration_ms",
			Help: "Workflow run duration (in milliseconds) of all workflow runs created in the last 1hr",
		},
		strings.Split(config.WorkflowFields, ","),
	)
	prometheus.MustRegister(runnersGauge)
	prometheus.MustRegister(runnersOrganizationGauge)
	prometheus.MustRegister(workflowRunStatusGauge)
	prometheus.MustRegister(workflowRunDurationGauge)
	prometheus.MustRegister(runnersEnterpriseGauge)

	prometheus.MustRegister(workflowJobDurationTotalGauge)
	prometheus.MustRegister(workflowJobStatusCounter)
	prometheus.MustRegister(rateLimitGauge)

	client, err = NewClient()
	if err != nil {
		log.Fatalln("Error: Client creation failed." + err.Error())
	}

	go periodicGithubFetcher()
	go getRunnersFromGithub()
	go getRunnersOrganizationFromGithub()
	go getWorkflowRunsFromGithub()
	go getRunnersEnterpriseFromGithub()
	go getRateLimitFromGithub()
}

// NewClient creates a Github Client
func NewClient() (*github.Client, error) {
	var (
		httpClient      *http.Client
		client          *github.Client
		cachedTransport *httpcache.Transport
	)

	cache := lrucache.New(config.Github.CacheSizeBytes, 0)
	cachedTransport = httpcache.NewTransport(cache)

	if len(config.Github.Token) > 0 {
		log.Printf("authenticating with Github Token")
		ctx := context.Background()
		ctx = context.WithValue(ctx, "HTTPClient", cachedTransport.Client())
		httpClient = oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: config.Github.Token}))
	} else {
		log.Printf("authenticating with Github App")
		transport, err := ghinstallation.NewKeyFromFile(cachedTransport, config.Github.AppID, config.Github.AppInstallationID, config.Github.AppPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("authentication failed: %v", err)
		}
		if config.Github.APIURL != "api.github.com" {
			githubAPIURL, err := getEnterpriseApiUrl(config.Github.APIURL)
			if err != nil {
				return nil, fmt.Errorf("enterprise url incorrect: %v", err)
			}
			transport.BaseURL = githubAPIURL
		}
		httpClient = &http.Client{Transport: transport}
	}

	if config.Github.APIURL != "api.github.com" {
		var err error
		client, err = github.NewEnterpriseClient(config.Github.APIURL, config.Github.APIURL, httpClient)
		if err != nil {
			return nil, fmt.Errorf("enterprise client creation failed: %v", err)
		}
	} else {
		client = github.NewClient(httpClient)
	}

	return client, nil
}

func getEnterpriseApiUrl(baseURL string) (string, error) {
	baseEndpoint, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(baseEndpoint.Path, "/") {
		baseEndpoint.Path += "/"
	}
	if !strings.HasSuffix(baseEndpoint.Path, "/api/v3/") &&
		!strings.HasPrefix(baseEndpoint.Host, "api.") &&
		!strings.Contains(baseEndpoint.Host, ".api.") {
		baseEndpoint.Path += "api/v3/"
	}

	// Trim trailing slash, otherwise there's double slash added to token endpoint
	return fmt.Sprintf("%s://%s%s", baseEndpoint.Scheme, baseEndpoint.Host, strings.TrimSuffix(baseEndpoint.Path, "/")), nil
}

func getRunnerLabelString(labels []string) string {
	var result string
	if len(labels) > 0 {
		for _, label := range labels {
			result += label + ","
		}
		result = strings.TrimSuffix(result, ",")
	}
	return result
}

func setCache(key string, value []byte, ttl int) {
	if err := cache.Set([]byte(key), value, ttl); err != nil {
		log.Printf("setCache: Error setting cache for key %s: %v", key, err)
	}
}

func getCache(key string) []byte {
	value, err := cache.Get([]byte(key))
	if err != nil {
		log.Printf("getCache: Error getting cache for key %s: %v", key, err)
		return nil
	}
	return value
}

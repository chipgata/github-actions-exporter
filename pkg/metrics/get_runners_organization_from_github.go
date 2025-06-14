package metrics

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/chipgata/github-actions-exporter/pkg/config"

	"github.com/google/go-github/v45/github"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	runnersOrganizationGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_runner_organization_status",
			Help: "runner status",
		},
		[]string{"organization", "os", "name", "id", "busy", "runner_labels"},
	)
)

func getAllOrgRunners(orga string) []*github.Runner {
	var runners []*github.Runner
	opt := &github.ListOptions{PerPage: 200}

	for {
		resp, rr, err := client.Actions.ListOrganizationRunners(context.Background(), orga, opt)
		if rl_err, ok := err.(*github.RateLimitError); ok {
			log.Printf("ListOrganizationRunners ratelimited. Pausing until %s", rl_err.Rate.Reset.Time.String())
			time.Sleep(time.Until(rl_err.Rate.Reset.Time))
			continue
		} else if err != nil {
			log.Printf("ListOrganizationRunners error for org %s: %s", orga, err.Error())
			return runners
		}

		runners = append(runners, resp.Runners...)
		if rr.NextPage == 0 {
			break
		}
		opt.Page = rr.NextPage
	}
	return runners
}

// getRunnersOrganizationFromGithub - return information about runners and their status for an organization
func getRunnersOrganizationFromGithub() {
	for {
		for _, orga := range config.Github.Organizations.Value() {
			runners := getAllOrgRunners(orga)
			for _, runner := range runners {
				runnerLabels := make([]string, 0, len(runner.Labels))
				for _, label := range runner.Labels {
					runnerLabels = append(runnerLabels, label.GetName())
				}
				runnerLabelString := getRunnerLabelString(runnerLabels)
				if runner.GetStatus() == "online" {
					runnersOrganizationGauge.WithLabelValues(orga, *runner.OS, *runner.Name, strconv.FormatInt(runner.GetID(), 10), strconv.FormatBool(runner.GetBusy()), runnerLabelString).Set(1)
				} else {
					runnersOrganizationGauge.WithLabelValues(orga, *runner.OS, *runner.Name, strconv.FormatInt(runner.GetID(), 10), strconv.FormatBool(runner.GetBusy()), runnerLabelString).Set(0)
				}
			}
		}

		time.Sleep(time.Duration(config.Github.Refresh) * time.Second)
		runnersOrganizationGauge.Reset()
	}
}

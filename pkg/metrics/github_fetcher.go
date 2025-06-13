package metrics

import (
	"context"
	"log"
	"time"

	"github.com/google/go-github/v45/github"

	"github.com/chipgata/github-actions-exporter/pkg/config"
)

var (
	repositories []string
)

func getAllReposForOrg(orga string) []string {
	var all_repos []string

	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{
			PerPage: 200,
			Page:    0,
		},
	}
	for {
		repos_page, resp, err := client.Repositories.ListByOrg(context.Background(), orga, opt)
		if rl_err, ok := err.(*github.RateLimitError); ok {
			log.Printf("ListByOrg ratelimited. Pausing until %s", rl_err.Rate.Reset.Time.String())
			time.Sleep(time.Until(rl_err.Rate.Reset.Time))
			continue
		} else if err != nil {
			log.Printf("ListByOrg error for %s: %s", orga, err.Error())
			break
		}
		for _, repo := range repos_page {
			all_repos = append(all_repos, *repo.FullName)
		}
		if resp.NextPage == 0 {
			break
		}
		opt.ListOptions.Page = resp.NextPage
	}
	return all_repos
}

func periodicGithubFetcher() {
	for {

		// Fetch repositories (if dynamic)
		var repos_to_fetch []string
		if len(config.Github.Repositories.Value()) > 0 {
			repos_to_fetch = config.Github.Repositories.Value()
		} else {
			for _, orga := range config.Github.Organizations.Value() {
				repos_to_fetch = append(repos_to_fetch, getAllReposForOrg(orga)...)
			}
		}
		repositories = repos_to_fetch

		time.Sleep(time.Duration(config.Github.Refresh) * 5 * time.Second)
	}
}

package metrics

import (
	"context"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/chipgata/github-actions-exporter/pkg/config"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/google/go-github/v45/github"
)

var (
	workflowJobDurationTotalGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "github_workflow_job_duration_total_ms",
		Help: "The total duration of jobs.",
	},
		[]string{"org", "repo", "branch", "status", "conclusion", "runner_group", "runner_labels", "workflow_name", "job_name", "job_id"},
	)

	workflowJobStatusCounter = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "github_workflow_job_status_count",
		Help: "Count of workflow job events.",
	},
		[]string{"org", "repo", "branch", "status", "conclusion", "runner_group", "runner_labels", "workflow_name", "job_name", "job_id"},
	)
)

// getFieldValue return value from run element which corresponds to field
func getFieldValue(repo string, run github.WorkflowRun, field string) string {
	switch field {
	case "repo":
		return repo
	case "id":
		return strconv.FormatInt(*run.ID, 10)
	case "node_id":
		return *run.NodeID
	case "head_branch":
		return *run.HeadBranch
	case "head_sha":
		return *run.HeadSHA
	case "run_number":
		return strconv.Itoa(*run.RunNumber)
	case "workflow_id":
		return strconv.FormatInt(*run.WorkflowID, 10)
	case "workflow":
		return *run.Name
	case "event":
		return *run.Event
	case "status":
		return *run.Status
	}
	log.Printf("Tried to fetch invalid field '%s'", field)
	return ""
}

func getRelevantFields(repo string, run *github.WorkflowRun) []string {
	relevantFields := strings.Split(config.WorkflowFields, ",")
	result := make([]string, len(relevantFields))
	for i, field := range relevantFields {
		result[i] = getFieldValue(repo, *run, field)
	}
	return result
}

func getRecentWorkflowRuns(owner string, repo string) []*github.WorkflowRun {
	window_start := time.Now().Add(time.Duration(-1) * time.Hour).Format(time.RFC3339)
	opt := &github.ListWorkflowRunsOptions{
		ListOptions: github.ListOptions{PerPage: 200},
		Created:     ">=" + window_start,
	}

	var runs []*github.WorkflowRun
	for {
		resp, rr, err := client.Actions.ListRepositoryWorkflowRuns(context.Background(), owner, repo, opt)
		if rl_err, ok := err.(*github.RateLimitError); ok {
			log.Printf("ListRepositoryWorkflowRuns ratelimited. Pausing until %s", rl_err.Rate.Reset.Time.String())
			time.Sleep(time.Until(rl_err.Rate.Reset.Time))
			continue
		} else if err != nil {
			log.Printf("ListRepositoryWorkflowRuns error for repo %s/%s: %s", owner, repo, err.Error())
			return runs
		}

		runs = append(runs, resp.WorkflowRuns...)
		if rr.NextPage == 0 {
			break
		}
		opt.Page = rr.NextPage
	}

	return runs
}

func getWorkflowJobs(owner string, repo string, runId int64) []*github.WorkflowJob {
	opt := &github.ListWorkflowJobsOptions{
		Filter:      "all",
		ListOptions: github.ListOptions{PerPage: 200},
	}

	var jobs []*github.WorkflowJob
	for {
		resp, rr, err := client.Actions.ListWorkflowJobs(context.Background(), owner, repo, runId, opt)
		if rl_err, ok := err.(*github.RateLimitError); ok {
			log.Printf("ListWorkflowJobs ratelimited. Pausing until %s", rl_err.Rate.Reset.Time.String())
			time.Sleep(time.Until(rl_err.Rate.Reset.Time))
			continue
		} else if err != nil {
			log.Printf("ListWorkflowJobs error for repo %s/%s: %s", owner, repo, err.Error())
			return jobs
		}

		jobs = append(jobs, resp.Jobs...)
		if rr.NextPage == 0 {
			break
		}
		opt.Page = rr.NextPage
	}

	return jobs
}

func getRunUsage(owner string, repo string, runId int64) *github.WorkflowRunUsage {
	for {
		resp, _, err := client.Actions.GetWorkflowRunUsageByID(context.Background(), owner, repo, runId)
		if rl_err, ok := err.(*github.RateLimitError); ok {
			log.Printf("GetWorkflowRunUsageByID ratelimited. Pausing until %s", rl_err.Rate.Reset.Time.String())
			time.Sleep(time.Until(rl_err.Rate.Reset.Time))
			continue
		} else if err != nil {
			log.Printf("GetWorkflowRunUsageByID error for repo %s/%s and runId %d: %s", owner, repo, runId, err.Error())
			return nil
		}
		return resp
	}
}

// getWorkflowRunsFromGithub - return informations and status about a workflow
func getWorkflowRunsFromGithub() {
	for {
		workflowRunStatusGauge.Reset()
		workflowRunDurationGauge.Reset()
		workflowJobDurationTotalGauge.Reset()
		workflowJobStatusCounter.Reset()
		total_runs := 0
		total_jobs := 0

		for _, repo := range repositories {
			r := strings.Split(repo, "/")
			runs := getRecentWorkflowRuns(r[0], r[1])
			total_runs += len(runs)

			for _, run := range runs {
				var s float64 = 0
				fields := getRelevantFields(repo, run)
				cacheWorkflowKey := r[1] + strconv.FormatInt(run.GetWorkflowID(), 10) + run.GetHeadSHA() + strconv.FormatInt(int64(run.GetRunNumber()), 10) + run.GetStatus() + run.GetConclusion()
				cacheWorkflowValue := getCache(cacheWorkflowKey)

				if cacheWorkflowValue == nil {
					log.Printf("Cache missed for workflow run %s/%s: %s", r[0], r[1], run.GetName())

					if run.GetConclusion() == "success" {
						s = 1
					} else if run.GetConclusion() == "skipped" {
						s = 2
					} else if run.GetConclusion() == "action_required" {
						s = 3
					} else if run.GetConclusion() == "cancelled" {
						s = 4
					} else if run.GetConclusion() == "failure" {
						s = 5
					} else if run.GetConclusion() == "neutral" {
						s = 6
					} else if run.GetConclusion() == "stale" {
						s = 7
					} else if run.GetConclusion() == "timed_out" {
						s = 8
					}

					workflowRunStatusGauge.WithLabelValues(fields...).Set(s)

					var run_usage *github.WorkflowRunUsage = nil
					if config.Metrics.FetchWorkflowRunUsage {
						run_usage = getRunUsage(r[0], r[1], *run.ID)
					}
					if run_usage == nil { // Fallback for Github Enterprise
						created := run.CreatedAt.Time.Unix()
						updated := run.UpdatedAt.Time.Unix()
						elapsed := updated - created
						workflowRunDurationGauge.WithLabelValues(fields...).Set(float64(elapsed * 1000))
					} else {
						workflowRunDurationGauge.WithLabelValues(fields...).Set(float64(run_usage.GetRunDurationMS()))
					}
				}

				jobs := getWorkflowJobs(r[0], r[1], *run.ID)
				total_jobs += len(jobs)
				for _, job := range jobs {
					cacheJobKey := r[1] + strconv.FormatInt(run.GetWorkflowID(), 10) + run.GetHeadSHA() + strconv.FormatInt(int64(run.GetRunNumber()), 10) + run.GetStatus() + run.GetConclusion() + job.GetConclusion() + strconv.FormatInt(job.GetID(), 10) + job.GetStatus()
					cacheJobValue := getCache(cacheJobKey)

					if cacheJobValue == nil {
						log.Printf("Cache missed for job run %s/%s: %s", r[0], r[1], job.GetName())

						runnerLabelString := getRunnerLabelString(job.Labels)
						if job.GetStatus() == "completed" {
							jobSeconds := math.Max(0, job.GetCompletedAt().Time.Sub(job.GetStartedAt().Time).Seconds())
							workflowJobDurationTotalGauge.WithLabelValues(
								r[0], r[1], run.GetHeadBranch(), job.GetStatus(), job.GetConclusion(),
								job.GetRunnerGroupName(), runnerLabelString, run.GetName(), job.GetName(), strconv.FormatInt(job.GetID(), 10),
							).Set(jobSeconds * 1000)
						}

						var j float64 = 0
						if job.GetConclusion() == "success" {
							j = 1
						} else if job.GetConclusion() == "failure" {
							j = 2
						} else if job.GetConclusion() == "cancelled" {
							j = 3
						} else if job.GetConclusion() == "skipped" {
							j = 4
						} else if job.GetConclusion() == "timed_out" {
							j = 5
						} else if job.GetConclusion() == "action_required" {
							j = 6
						} else if job.GetConclusion() == "neutral" {
							j = 7
						}
						workflowJobStatusCounter.WithLabelValues(r[0], r[1], run.GetHeadBranch(), job.GetStatus(), job.GetConclusion(), job.GetRunnerGroupName(), runnerLabelString, run.GetName(), job.GetName(), strconv.FormatInt(job.GetID(), 10)).Set(j)
					}
					setCache(cacheJobKey, []byte("1"), 3600)
				}

				// Cache the result
				setCache(cacheWorkflowKey, []byte("1"), 3600)
			}
		}

		time.Sleep(time.Duration(config.Github.Refresh) * time.Second)
	}
}

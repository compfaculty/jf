package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/alitto/pond"

	"jf/internal/aggregators"
	"jf/internal/config"
	"jf/internal/feed"
	"jf/internal/models"
	"jf/internal/repo"
	"jf/internal/scrape"
	commonpkg "jf/internal/scrape/common"
)

// StartRateLimitRetryWorker runs a background goroutine that periodically retries
// jobs queued for 429 rate limit. Default: every 30 min, backoff 2h on 429.
func StartRateLimitRetryWorker(
	ctx context.Context,
	r repo.Repo,
	cfg *config.Config,
	aggregatorReg *aggregators.Registry,
	broker *Broker,
	wp *pond.WorkerPool,
) {
	interval := 30 * time.Minute
	if v := os.Getenv("JF_RATE_LIMIT_RETRY_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		}
	}
	waitHours := 2
	if v := os.Getenv("JF_RATE_LIMIT_WAIT_HOURS"); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h > 0 {
			waitHours = h
		}
	}
	backoff := time.Duration(waitHours) * time.Hour

	feedParser := feed.NewParser(http.DefaultClient)
	var bp commonpkg.Browser

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Printf("[RATE_LIMIT_RETRY] worker stopping")
				return
			case <-ticker.C:
				runRetry(ctx, r, cfg, aggregatorReg, broker, wp, feedParser, bp, backoff)
			}
		}
	}()
	log.Printf("[RATE_LIMIT_RETRY] worker started interval=%s backoff=%s", interval, backoff)
}

func runRetry(
	ctx context.Context,
	r repo.Repo,
	cfg *config.Config,
	aggregatorReg *aggregators.Registry,
	broker *Broker,
	wp *pond.WorkerPool,
	feedParser *feed.Parser,
	bp commonpkg.Browser,
	backoff time.Duration,
) {
	entries, err := r.ListRateLimitedReady(ctx)
	if err != nil {
		log.Printf("[RATE_LIMIT_RETRY] ListRateLimitedReady error: %v", err)
		return
	}
	if len(entries) == 0 {
		return
	}

	companies, err := r.ListCompanies(ctx)
	if err != nil {
		log.Printf("[RATE_LIMIT_RETRY] ListCompanies error: %v", err)
		return
	}
	companyMap := make(map[string]models.Company)
	for _, c := range companies {
		companyMap[c.ID] = c
	}

	aggregators := aggregatorReg.GetAll()
	aggregatorMap := make(map[string]models.Aggregator)
	for _, a := range aggregators {
		aggregatorMap[a.Name] = a
	}

	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.JobID)
	}
	jobs, err := r.ListJobsByIDs(ctx, ids)
	if err != nil {
		log.Printf("[RATE_LIMIT_RETRY] ListJobsByIDs error: %v", err)
		return
	}
	jobMap := make(map[string]models.Job)
	for _, j := range jobs {
		jobMap[j.ID] = j
	}

	for _, e := range entries {
		j, ok := jobMap[e.JobID]
		if !ok {
			_ = r.DequeueRateLimited(ctx, e.JobID)
			log.Printf("[RATE_LIMIT_RETRY] job %s not found, dequeued", e.JobID)
			continue
		}
		if j.Applied {
			_ = r.DequeueRateLimited(ctx, e.JobID)
			log.Printf("[RATE_LIMIT_RETRY] job %s already applied, dequeued", e.JobID)
			continue
		}

		var source scrape.JobSource
		if j.AggregatorName != "" {
			if agg, exists := aggregatorMap[j.AggregatorName]; exists {
				company := models.Company{
					Name:       agg.Name,
					CareersURL: agg.SourceURL,
					Active:     agg.Active,
				}
				if err := r.UpsertCompanyByName(ctx, &company); err == nil {
					source = scrape.NewJobSource(company, &agg, http.DefaultClient, bp, wp, feedParser, r)
				}
			}
		} else if j.CompanyID != "" {
			if company, exists := companyMap[j.CompanyID]; exists {
				source = scrape.NewJobSource(company, nil, http.DefaultClient, bp, wp, feedParser, r)
			}
		}
		if source == nil && j.URL != "" {
			if agg := findAggregatorByURL(j.URL, aggregators); agg != nil {
				company := models.Company{
					Name:       agg.Name,
					CareersURL: agg.SourceURL,
					Active:     agg.Active,
				}
				if err := r.UpsertCompanyByName(ctx, &company); err == nil {
					source = scrape.NewJobSource(company, agg, http.DefaultClient, bp, wp, feedParser, r)
				}
			}
		}

		if source == nil {
			log.Printf("[RATE_LIMIT_RETRY] job %s no source, skipping", e.JobID)
			continue
		}

		result, err := source.ApplyJob(ctx, j, cfg)
		if err != nil {
			log.Printf("[RATE_LIMIT_RETRY] job %s ApplyJob error: %v", e.JobID, err)
			continue
		}
		if result == nil {
			log.Printf("[RATE_LIMIT_RETRY] job %s unsupported", e.JobID)
			continue
		}

		if result.OK {
			if deqErr := r.DequeueRateLimited(ctx, e.JobID); deqErr != nil {
				log.Printf("[RATE_LIMIT_RETRY] DequeueRateLimited error: %v", deqErr)
			}
			if _, applyErr := r.ApplyJobs(ctx, []string{e.JobID}); applyErr != nil {
				log.Printf("[RATE_LIMIT_RETRY] ApplyJobs error: %v", applyErr)
			} else {
				log.Printf("[RATE_LIMIT_RETRY] job %s applied ok", e.JobID)
				if broker != nil {
					broker.SendApplyComplete(1, 0)
				}
			}
		} else if result.Status == 429 {
			retryAfter := time.Now().UTC().Add(backoff)
			if updErr := r.UpdateRateLimitedRetry(ctx, e.JobID, retryAfter); updErr != nil {
				log.Printf("[RATE_LIMIT_RETRY] UpdateRateLimitedRetry error: %v", updErr)
			} else {
				log.Printf("[RATE_LIMIT_RETRY] job %s 429 again, retry at %s", e.JobID, retryAfter.Format(time.RFC3339))
			}
		} else {
			log.Printf("[RATE_LIMIT_RETRY] job %s failed status=%d msg=%s", e.JobID, result.Status, result.Message)
		}
	}
}

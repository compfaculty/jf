package companysite

import (
	"context"
	"strings"

	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/scrape/common"
	util "jf/internal/utils"

	"github.com/alitto/pond"
)

type AkeylessScraper struct {
	company models.Company
	client  common.Doer
	wp      *pond.WorkerPool // optional shared pool
}

func NewAkeyless(c models.Company, client common.Doer, wp *pond.WorkerPool) *AkeylessScraper {
	return &AkeylessScraper{
		company: c,
		client:  common.EnsureClient(client),
		wp:      wp,
	}
}

func (s *AkeylessScraper) GetJobs(ctx context.Context, _ *config.Config) ([]models.ScrapedJob, error) {
	root := strings.TrimSpace(s.company.CareersURL)
	if root == "" {
		return nil, nil
	}

	cm := NewComeet(s.client).WithPool(s.wp)

	links, err := cm.Discover(ctx, root)
	if err != nil || len(links) == 0 {
		return nil, err
	}

	// Use bulk fetch via the pool
	results := cm.FetchAll(ctx, links)

	jobs := make([]models.ScrapedJob, 0, len(results))
	for _, r := range results {
		if r.Err != nil || strings.TrimSpace(r.URL) == "" {
			continue
		}
		jobs = append(jobs, models.ScrapedJob{
			Title:       r.Title,
			URL:         util.CanonURL(r.URL),
			Location:    "",
			Description: strings.TrimSpace(r.HTML),
		})
	}
	return common.DedupeScraped(jobs), nil
}

// GetJobPosted extracts the posted date from a job URL.
// Stub implementation - returns empty string until instructed where/how to find the date.
func (s *AkeylessScraper) GetJobPosted(ctx context.Context, jobURL string) (string, error) {
	return "", nil
}

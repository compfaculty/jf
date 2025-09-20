package scrape

import (
	"context"
	"strings"

	"jf/internal/config"
	"jf/internal/models"
	util "jf/internal/utils"

	"github.com/alitto/pond"
)

type AkeylessScraper struct {
	company models.Company
	client  Doer
	wp      *pond.WorkerPool // optional shared pool
}

func NewAkeyless(c models.Company, client Doer, wp *pond.WorkerPool) *AkeylessScraper {
	return &AkeylessScraper{
		company: c,
		client:  ensureClient(client),
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
	return dedupeScraped(jobs), nil
}

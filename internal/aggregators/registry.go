package aggregators

import (
	"jf/internal/models"
	"strings"
	"time"
)

// embeddedAggregators contains job boards/aggregators that are hardcoded.
// These aggregate jobs from multiple companies.
// For each aggregator:
// - Name: display name
// - SourceURL: URL to scrape (for scraping) or RSS feed URL (for RSS feeds)
// - Type: "scraper" or "rss_feed"
var embeddedAggregators = []struct {
	Name      string
	SourceURL string
	Type      string
}{
	// RSS Feed aggregators
	//{"DOU Jobs", "https://jobs.dou.ua/vacancies/feeds/", "rss_feed"},
	//{"Jobicy", "https://jobicy.com/feed/job_feed", "rss_feed"},
	//{"Real Work From Anywhere", "https://www.realworkfromanywhere.com/rss.xml", "rss_feed"},
	// Job board aggregators (scrapers)
	{"Secret TLV", "https://jobs.secrettelaviv.com/", "scraper"},
	{"Telfed Job Board", "https://www.telfed.org.il/job-board/", "scraper"},
}

// Registry holds in-memory aggregators loaded from embeddedAggregators.
type Registry struct {
	aggregators []models.Aggregator
}

// NewRegistry creates a new registry and loads aggregators from embeddedAggregators.
func NewRegistry() *Registry {
	registry := &Registry{
		aggregators: make([]models.Aggregator, 0, len(embeddedAggregators)),
	}

	for _, e := range embeddedAggregators {
		name := strings.TrimSpace(e.Name)
		sourceURL := strings.TrimSpace(e.SourceURL)
		aggType := strings.TrimSpace(e.Type)
		if name == "" || sourceURL == "" {
			continue
		}
		if aggType == "" {
			aggType = "scraper" // default to scraper
		}

		registry.aggregators = append(registry.aggregators, models.Aggregator{
			ID:        "", // Not used for in-memory aggregators
			Name:      name,
			SourceURL: sourceURL,
			Type:      aggType,
			Active:    true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		})
	}

	return registry
}

// GetAll returns all aggregators.
func (r *Registry) GetAll() []models.Aggregator {
	return r.aggregators
}

// GetByName returns an aggregator by name, or nil if not found.
func (r *Registry) GetByName(name string) *models.Aggregator {
	for i := range r.aggregators {
		if r.aggregators[i].Name == name {
			return &r.aggregators[i]
		}
	}
	return nil
}

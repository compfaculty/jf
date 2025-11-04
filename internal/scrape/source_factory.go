package scrape

import (
	"jf/internal/feed"
	"jf/internal/models"
	"jf/internal/repo"
	commonpkg "jf/internal/scrape/common"
	"strings"

	"github.com/alitto/pond"
)

// NewJobSource creates an appropriate JobSource based on the input.
// For companies: returns CompanySource
// For aggregators: returns BoardSource (if type="scraper") or RSSSource (if type="rss_feed")
// Pass nil for repo to skip URL existence checks in scrapers.
func NewJobSource(
	c models.Company,
	agg *models.Aggregator,
	client commonpkg.Doer,
	browser commonpkg.Browser,
	wp *pond.WorkerPool,
	parser *feed.Parser,
	r repo.Repo,
) JobSource {
	// If aggregator is provided, route based on type
	if agg != nil {
		switch strings.ToLower(strings.TrimSpace(agg.Type)) {
		case "rss_feed":
			return NewRSSSource(*agg, parser, browser)

		case "scraper":
			// This is a job board - create board source
			// Convert aggregator to Company-like structure for scraper creation
			company := models.Company{
				Name:       agg.Name,
				CareersURL: agg.SourceURL,
				Active:     agg.Active,
			}
			scraper := NewJobScraper(company, client, browser, wp, r)
			return NewBoardSource(*agg, scraper, client, browser)

		default:
			// Unknown type, treat as company
			return createCompanySource(c, client, browser, wp, r)
		}
	}

	// No aggregator - this is a direct company
	return createCompanySource(c, client, browser, wp, r)
}

// createCompanySource creates a CompanySource from a company.
func createCompanySource(c models.Company, client commonpkg.Doer, browser commonpkg.Browser, wp *pond.WorkerPool, r repo.Repo) *CompanySource {
	scraper := NewJobScraper(c, client, browser, wp, r)
	return NewCompanySource(c, scraper, client, browser, wp)
}

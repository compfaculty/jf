package scrape

import (
	"jf/internal/feed"
	"jf/internal/models"
	commonpkg "jf/internal/scrape/common"
	"strings"

	"github.com/alitto/pond"
)

// NewJobSource creates an appropriate JobSource based on the input.
// For companies: returns CompanySource
// For aggregators: returns BoardSource (if type="scraper") or RSSSource (if type="rss_feed")
func NewJobSource(
	c models.Company,
	agg *models.Aggregator,
	client commonpkg.Doer,
	browser commonpkg.Browser,
	wp *pond.WorkerPool,
	parser *feed.Parser,
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
			scraper := NewJobScraper(company, client, browser, wp)
			return NewBoardSource(*agg, scraper, client, browser)

		default:
			// Unknown type, treat as company
			return createCompanySource(c, client, browser, wp)
		}
	}

	// No aggregator - this is a direct company
	return createCompanySource(c, client, browser, wp)
}

// createCompanySource creates a CompanySource from a company.
func createCompanySource(c models.Company, client commonpkg.Doer, browser commonpkg.Browser, wp *pond.WorkerPool) *CompanySource {
	scraper := NewJobScraper(c, client, browser, wp)
	return NewCompanySource(c, scraper, client, browser, wp)
}

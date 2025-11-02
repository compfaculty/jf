package scrape

import (
	"jf/internal/models"
	"jf/internal/scrape/common"
	"strings"

	companysite "jf/internal/scrape/company-site"
	jobboards "jf/internal/scrape/job-boards"

	"github.com/alitto/pond"
)

// NewJobScraper chooses a concrete scraper by careers host.
// Pass nil for client to use the default robust httpx client.
// Pass nil for browser if you don't want JS fallback in Generic.
func NewJobScraper(c models.Company, client common.Doer, browser common.Browser, wp *pond.WorkerPool) common.JobScraper {
	switch {
	case strings.Contains(c.CareersURL, "telfed.org.il/job-board") || strings.Contains(c.CareersURL, "telfed.org.il/job"):
		// Telfed requires BrowserPool due to Cloudflare protection
		if browser == nil {
			// Fallback to generic if no browser (will likely fail due to Cloudflare)
			return companysite.NewGeneric(c, client, browser, wp)
		}
		// Note: avoid log import here to keep factory lean; rely on Telfed scraper logs
		return jobboards.NewTelfed(c, browser, wp)
	case strings.Contains(c.CareersURL, "secrettelaviv.com"):
		return jobboards.NewSecretTelAviv(c, client)
	case strings.Contains(c.CareersURL, "40seas.com"):
		return companysite.NewFortySeas(c, client)
	case strings.Contains(c.CareersURL, "agrematch.com"):
		return companysite.NewAgrematch(c, client)
	case strings.Contains(c.CareersURL, "ai21.com"):
		return companysite.NewAi21(c, client)
	case strings.Contains(c.CareersURL, "akeyless.io"):
		return companysite.NewAkeyless(c, client, wp)
	case strings.Contains(c.CareersURL, "audiocodes.com"):
		return companysite.NewAudioCodes(c, client)
	default:
		// generic path: static first, optional browser fallback
		return companysite.NewGeneric(c, client, browser, wp)
	}
}

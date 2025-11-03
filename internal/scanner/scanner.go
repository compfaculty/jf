package scanner

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"jf/internal/config"
	"jf/internal/feed"
	"jf/internal/models"
	"jf/internal/pool"
	"jf/internal/repo"
	"jf/internal/scrape"
	commonpkg "jf/internal/scrape/common"
	"jf/internal/utils"

	"github.com/alitto/pond"
)

type Scanner struct {
	repo repo.Repo
	cfg  *config.Config
	http commonpkg.Doer
	bp   *pool.BrowserPool
	wp   *pond.WorkerPool

	mu     sync.Mutex
	state  ScanState
	cancel context.CancelFunc
}

type ScanState struct {
	Running   bool      `json:"running"`
	StartedAt time.Time `json:"started_at"`
	Percent   int       `json:"percent"`
	Found     int       `json:"found"`
	Total     int       `json:"total"`
	Error     string    `json:"error"`
}

// NewScanner wires both pools. If you don’t need one of them yet, you can pass nil;
// it’ll still work (we gate usage accordingly).
func NewScanner(r repo.Repo, cfg *config.Config, httpDoer commonpkg.Doer, bp *pool.BrowserPool, wp *pond.WorkerPool) *Scanner {
	return &Scanner{repo: r, cfg: cfg, http: httpDoer, bp: bp, wp: wp}
}

func (m *Scanner) Status() ScanState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Stop cancels an active scan (idempotent).
func (m *Scanner) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Scanner) StartScan() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Running {
		return nil // idempotent
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.state = ScanState{
		Running:   true,
		StartedAt: time.Now().UTC(),
		Percent:   0,
		Found:     0,
		Total:     0,
		Error:     "",
	}
	go m.run(ctx)
	return nil
}

func (m *Scanner) run(ctx context.Context) {
	utils.Verbosef("Scanner: starting scan")

	// Load all three source types
	companies, err := m.repo.ListCompanies(ctx)
	if err != nil {
		m.finishWithErr(err)
		return
	}
	utils.Verbosef("Scanner: loaded %d companies", len(companies))

	aggregators, err := m.repo.ListAggregators(ctx)
	if err != nil {
		m.finishWithErr(err)
		return
	}
	utils.Verbosef("Scanner: loaded %d aggregators", len(aggregators))

	// Separate aggregators by type
	var jobBoards []models.Aggregator
	var rssFeeds []models.Aggregator
	for _, agg := range aggregators {
		aggType := strings.ToLower(strings.TrimSpace(agg.Type))
		if aggType == "scraper" {
			jobBoards = append(jobBoards, agg)
		} else if aggType == "rss_feed" {
			rssFeeds = append(rssFeeds, agg)
		}
	}

	// Total sources to process
	total := len(companies) + len(jobBoards) + len(rssFeeds)
	m.setTotal(total)
	utils.Verbosef("Scanner: total sources to process: %d (companies=%d, jobBoards=%d, rssFeeds=%d)",
		total, len(companies), len(jobBoards), len(rssFeeds))
	if total == 0 {
		m.finishOK(100, 0)
		return
	}

	// Create RSS parser for RSS sources
	feedParser := feed.NewParser(m.http)

	var wg sync.WaitGroup
	var found int64 // total jobs
	var done int64  // sources finished

	// Process companies
	for _, c := range companies {
		c := c
		utils.Verbosef("Scanner: processing company: %s", c.Name)
		source := scrape.NewJobSource(c, nil, m.http, m.bp, m.wp, feedParser)
		m.processSource(ctx, &wg, &found, &done, total, source, &c, nil)
	}

	// Process job boards
	for _, agg := range jobBoards {
		agg := agg
		utils.Verbosef("Scanner: processing job board: %s", agg.Name)
		// Ensure aggregator has a corresponding company entry
		company := &models.Company{
			Name:       agg.Name,
			CareersURL: agg.SourceURL,
			Active:     agg.Active,
		}
		if err := m.repo.UpsertCompanyByName(ctx, company); err != nil {
			m.appendWarn(fmt.Sprintf("%s: failed to upsert company: %v", agg.Name, err))
			continue
		}
		source := scrape.NewJobSource(*company, &agg, m.http, m.bp, m.wp, feedParser)
		m.processSource(ctx, &wg, &found, &done, total, source, company, &agg)
	}

	// Process RSS feeds
	for _, agg := range rssFeeds {
		agg := agg
		utils.Verbosef("Scanner: processing RSS feed: %s", agg.Name)
		c := models.Company{} // Empty for RSS
		source := scrape.NewJobSource(c, &agg, m.http, m.bp, m.wp, feedParser)
		m.processSource(ctx, &wg, &found, &done, total, source, nil, &agg)
	}

	wg.Wait()

	select {
	case <-ctx.Done():
		m.finishWithErr(ctx.Err())
	default:
		m.finishOK(100, int(atomic.LoadInt64(&found)))
	}
}

// processSource processes a single source using the unified JobSource interface.
func (m *Scanner) processSource(
	ctx context.Context,
	wg *sync.WaitGroup,
	found *int64,
	done *int64,
	total int,
	source scrape.JobSource,
	company *models.Company,
	aggregator *models.Aggregator,
) {
	wg.Add(1)
	runOne := func() {
		defer wg.Done()

		name := ""
		if company != nil {
			name = company.Name
		} else if aggregator != nil {
			name = aggregator.Name
		}

		cctx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()

		utils.Verbosef("Scanner: starting FindJobs for %s", name)
		// Step 1: FindJobs - discover job listings
		listings, err := source.FindJobs(cctx, m.cfg)
		if err != nil && cctx.Err() == nil {
			m.appendWarn(fmt.Sprintf("%s: FindJobs error: %v", name, err))
		} else {
			utils.Verbosef("Scanner: FindJobs found %d listings for %s", len(listings), name)
		}

		// Step 2: ParseJobMetadata - extract detailed metadata for each listing
		newN := 0
		for i, listing := range listings {
			select {
			case <-cctx.Done():
				m.appendWarn(fmt.Sprintf("cancelled: %v", cctx.Err()))
				return
			default:
			}

			// Parse metadata
			utils.Verbosef("Scanner: parsing metadata for listing %d/%d from %s", i+1, len(listings), name)
			metadata, err := source.ParseJobMetadata(cctx, listing)
			if err != nil {
				utils.Verbosef("Scanner: metadata parsing failed for listing %d from %s: %v", i+1, name, err)
				continue // Skip this job if metadata parsing fails
			}
			utils.Verbosef("Scanner: parsed metadata: title=%q url=%q company=%q location=%q",
				metadata.Title, metadata.URL, metadata.Company, metadata.Location)

			// Determine company ID
			var companyID string
			if company != nil {
				companyID = company.ID
			} else if aggregator != nil {
				// For aggregators, we need to get/create company
				if metadata.Company != "" {
					// Create/get company for this job
					c := &models.Company{
						Name:       metadata.Company,
						CareersURL: metadata.URL, // Use job URL as fallback
						Active:     true,
					}
					if err := m.repo.UpsertCompanyByName(cctx, c); err == nil {
						companyID = c.ID
					}
				} else {
					// Use aggregator's company entry
					c := &models.Company{
						Name:       aggregator.Name,
						CareersURL: aggregator.SourceURL,
						Active:     aggregator.Active,
					}
					if err := m.repo.UpsertCompanyByName(cctx, c); err == nil {
						companyID = c.ID
					}
				}
			}

			if companyID == "" {
				continue // Skip if we can't get a company ID
			}

			// Step 3: Store in DB
			j := utils.GetJob()
			j.CompanyID = companyID
			j.Title = strings.TrimSpace(metadata.Title)
			j.URL = strings.TrimSpace(metadata.URL)
			j.Location = strings.TrimSpace(metadata.Location)
			j.Description = strings.TrimSpace(metadata.Description)
			j.HREmail = strings.TrimSpace(metadata.HREmail)
			j.HRPhone = strings.TrimSpace(metadata.HRPhone)
			j.ApplyURL = strings.TrimSpace(metadata.ApplyURL)
			j.ApplyViaPortal = metadata.ApplyViaPortal
			if !metadata.DatePosted.IsZero() {
				j.PostedAt = metadata.DatePosted.Format(time.RFC3339)
			}

			if aggregator != nil {
				j.SourceID = aggregator.ID
				j.AggregatorName = aggregator.Name
			}

			if err := m.repo.UpsertJob(cctx, j); err != nil {
				m.appendWarn(fmt.Sprintf("%s: upsert %q failed: %v", name, j.URL, err))
				utils.PutJob(j)
				continue
			}

			utils.Verbosef("Scanner: upserted job: title=%q url=%q company_id=%s", j.Title, j.URL, j.CompanyID)
			utils.PutJob(j)
			newN++
		}

		totalFound := atomic.AddInt64(found, int64(newN))
		curDone := int(atomic.AddInt64(done, 1))
		pct := (curDone * 100) / total
		m.setProgress(pct, int(totalFound))

		log.Printf("[SCAN] %-22s jobs=%d (%d/%d, %d%%)", name, newN, curDone, total, pct)
		utils.Verbosef("Scanner: completed processing %s: new jobs=%d total found so far=%d", name, newN, totalFound)
	}

	if m.wp != nil {
		m.wp.Submit(runOne)
	} else {
		runOne()
	}
}

func (m *Scanner) setTotal(total int) {
	m.mu.Lock()
	m.state.Total = total
	m.mu.Unlock()
}

func (m *Scanner) setProgress(percent, found int) {
	m.mu.Lock()
	m.state.Percent = percent
	m.state.Found = found
	m.mu.Unlock()
}

func (m *Scanner) appendWarn(e string) {
	if strings.TrimSpace(e) == "" {
		return
	}
	m.mu.Lock()
	// Keep last message visible; you can aggregate if you prefer.
	m.state.Error = e
	m.mu.Unlock()
}

func (m *Scanner) finishWithErr(err error) {
	m.mu.Lock()
	m.state.Running = false
	m.state.Error = err.Error()
	m.mu.Unlock()
}

func (m *Scanner) finishOK(pct, found int) {
	m.mu.Lock()
	m.state.Running = false
	m.state.Percent = pct
	m.state.Found = found
	m.mu.Unlock()
}

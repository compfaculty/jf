package feed

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/repo"
)

// FeedUpdate represents a single feed update event
type FeedUpdate struct {
	FeedName    string    `json:"feed_name"`
	FeedURL     string    `json:"feed_url"`
	JobsFound   int       `json:"jobs_found"`
	JobsNew     int       `json:"jobs_new"`
	JobsUpdated int       `json:"jobs_updated"`
	Status      string    `json:"status"` // "success", "error"
	Error       string    `json:"error,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Monitor manages RSS feed polling and job ingestion
type Monitor struct {
	repo       repo.Repo
	parser     *Parser
	cfg        *config.Config
	updates    []FeedUpdate // recent updates (keep last N)
	mu         sync.RWMutex
	lastUpdate atomic.Value // time.Time
	ticker     *time.Ticker
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewMonitor creates a new RSS feed monitor
func NewMonitor(r repo.Repo, parser *Parser, cfg *config.Config) *Monitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Monitor{
		repo:    r,
		parser:  parser,
		cfg:     cfg,
		updates: make([]FeedUpdate, 0, 100), // Keep last 100 updates
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins monitoring feeds according to the configured interval
// Now reads RSS feed aggregators from the database instead of config
func (m *Monitor) Start() error {
	ctx := context.Background()

	// Get all RSS feed aggregators from database
	aggregators, err := m.repo.ListAggregators(ctx)
	if err != nil {
		log.Printf("[FEED] Failed to list aggregators: %v", err)
		return err
	}

	// Filter for RSS feed type aggregators only
	rssAggregators := make([]models.Aggregator, 0)
	for _, agg := range aggregators {
		if agg.Type == "rss_feed" && agg.Active {
			rssAggregators = append(rssAggregators, agg)
		}
	}

	if len(rssAggregators) == 0 {
		log.Printf("[FEED] No RSS feed aggregators found in database")
		return nil
	}

	interval := m.cfg.RSSPollIntervalDuration()
	log.Printf("[FEED] Starting monitor with interval=%s, feeds=%d", interval, len(rssAggregators))

	// Do initial poll
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.pollOnce()
	}()

	// Start periodic polling
	m.ticker = time.NewTicker(interval)
	m.wg.Add(1)
	go m.run()

	return nil
}

// Stop stops the monitor
func (m *Monitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.ticker != nil {
		m.ticker.Stop()
	}
	m.wg.Wait()
	log.Printf("[FEED] Monitor stopped")
}

func (m *Monitor) run() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.ticker.C:
			m.pollOnce()
		}
	}
}

func (m *Monitor) pollOnce() {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	defer cancel()

	// Get RSS feed aggregators from database
	aggregators, err := m.repo.ListAggregators(ctx)
	if err != nil {
		log.Printf("[FEED] Failed to list aggregators: %v", err)
		return
	}

	// Filter for active RSS feed aggregators
	rssAggregators := make([]models.Aggregator, 0)
	for _, agg := range aggregators {
		if agg.Type == "rss_feed" && agg.Active {
			rssAggregators = append(rssAggregators, agg)
		}
	}

	for _, agg := range rssAggregators {
		update := m.pollFeed(ctx, agg)
		m.addUpdate(update)
	}

	m.lastUpdate.Store(time.Now())
}

func (m *Monitor) pollFeed(ctx context.Context, agg models.Aggregator) FeedUpdate {
	update := FeedUpdate{
		FeedName:  agg.Name,
		FeedURL:   agg.SourceURL,
		UpdatedAt: time.Now(),
	}

	log.Printf("[FEED] Polling %s (%s)", agg.Name, agg.SourceURL)

	items, err := m.parser.ParseFeed(ctx, agg.SourceURL)
	if err != nil {
		update.Status = "error"
		update.Error = err.Error()
		log.Printf("[FEED] Error polling %s: %v", agg.Name, err)
		return update
	}

	// Convert RSS items to jobs
	rssJobs := ConvertItemsToJobs(items, agg.Name, agg.SourceURL)
	update.JobsFound = len(rssJobs)

	// Ensure company/companies exist
	// For feed sources like Jobicy, each job may have its own company
	// Map company names to their IDs
	companyCache := make(map[string]string)

	// Helper function to get or create company ID
	getCompanyID := func(companyName string) (string, error) {
		if companyName == "" {
			companyName = agg.Name
		}

		// Check cache first
		if id, exists := companyCache[companyName]; exists {
			return id, nil
		}

		// Upsert the company
		company := &models.Company{
			Name:       companyName,
			CareersURL: agg.SourceURL, // Use aggregator source URL as fallback
			Active:     true,
		}
		if err := m.repo.UpsertCompanyByName(ctx, company); err != nil {
			return "", fmt.Errorf("upsert company %s: %w", companyName, err)
		}

		// Verify the company ID is valid (UpsertCompanyByName should set it)
		if company.ID == "" {
			return "", fmt.Errorf("company %s has empty ID after upsert", companyName)
		}

		// Cache it
		companyCache[companyName] = company.ID
		return company.ID, nil
	}

	// Upsert jobs (UpsertJob uses ON CONFLICT so duplicates are handled automatically)
	// Note: We can't easily distinguish new vs updated from UpsertJob alone,
	// so we'll estimate based on the assumption that most will be new
	newCount := 0
	for _, job := range rssJobs {
		companyName := job.CompanyName
		if companyName == "" {
			companyName = agg.Name
		}

		// Get or create company ID (retry on failure)
		companyID, err := getCompanyID(companyName)
		if err != nil {
			log.Printf("[FEED] Error getting company ID for %s: %v, skipping job %s", companyName, err, job.URL)
			continue
		}

		job.CompanyID = companyID
		job.SourceID = agg.ID // Set the aggregator source ID

		if err := m.repo.UpsertJob(ctx, &job); err != nil {
			log.Printf("[FEED] Error upserting job %s: %v", job.URL, err)
			continue
		}

		// Since we can't easily tell if it was insert vs update without querying,
		// we'll assume all successful upserts are new for now
		// (In practice, ON CONFLICT UPDATE means most will be updates after first run)
		newCount++
	}

	// After first poll, most jobs will be updates. For simplicity, treat all as potentially new.
	// In a real implementation, you could query before/after to track changes.
	update.JobsNew = newCount
	update.JobsUpdated = 0 // We can't easily distinguish without before/after query
	update.Status = "success"
	log.Printf("[FEED] %s: found=%d upserted=%d", agg.Name, update.JobsFound, newCount)

	return update
}

// addUpdate adds an update to the history (keeping last 100)
func (m *Monitor) addUpdate(update FeedUpdate) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.updates = append(m.updates, update)
	if len(m.updates) > 100 {
		m.updates = m.updates[1:]
	}
}

// GetUpdates returns recent feed updates
func (m *Monitor) GetUpdates(limit int) []FeedUpdate {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > len(m.updates) {
		limit = len(m.updates)
	}

	// Return last N updates (most recent first)
	start := len(m.updates) - limit
	if start < 0 {
		start = 0
	}

	result := make([]FeedUpdate, limit)
	copy(result, m.updates[start:])

	// Reverse to show most recent first
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

// GetLastUpdateTime returns when feeds were last polled
func (m *Monitor) GetLastUpdateTime() time.Time {
	if v := m.lastUpdate.Load(); v != nil {
		return v.(time.Time)
	}
	return time.Time{}
}

// GetStatus returns current monitor status
func (m *Monitor) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get RSS feed aggregators count from database
	ctx := context.Background()
	aggregators, err := m.repo.ListAggregators(ctx)
	totalFeeds := 0
	enabledFeeds := 0
	if err == nil {
		for _, agg := range aggregators {
			if agg.Type == "rss_feed" {
				totalFeeds++
				if agg.Active {
					enabledFeeds++
				}
			}
		}
	}

	return map[string]interface{}{
		"enabled":        enabledFeeds > 0,
		"total_feeds":    totalFeeds,
		"enabled_feeds":  enabledFeeds,
		"poll_interval":  m.cfg.RSSPollIntervalDuration().String(),
		"last_update":    m.GetLastUpdateTime(),
		"recent_updates": len(m.updates),
	}
}

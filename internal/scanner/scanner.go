package scanner

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/repo"
	"jf/internal/scrape"
)

type Manager struct {
	repo  *repo.SQLiteRepo
	prefs *config.Config
	http  scrape.Doer

	mu     sync.Mutex
	state  scanState
	cancel context.CancelFunc
}

type scanState struct {
	Running   bool      `json:"running"`
	StartedAt time.Time `json:"started_at"`
	Percent   int       `json:"percent"`
	Found     int       `json:"found"`
	Total     int       `json:"total"`
	Error     string    `json:"error"`
}

func NewManager(r *repo.SQLiteRepo, prefs *config.Config, httpDoer scrape.Doer) *Manager {
	return &Manager{repo: r, prefs: prefs, http: httpDoer}
}

func (m *Manager) Status() scanState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *Manager) StartScan() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Running {
		return nil // idempotent
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.state = scanState{
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

func (m *Manager) run(ctx context.Context) {
	companies, err := m.repo.ListCompanies(ctx)
	if err != nil {
		m.finishWithErr(err)
		return
	}
	total := len(companies)
	m.setTotal(total)
	if total == 0 {
		m.finishOK(100, 0)
		return
	}

	found := 0
	for i, c := range companies {
		select {
		case <-ctx.Done():
			m.finishWithErr(ctx.Err())
			return
		default:
		}

		comp := scrape.Company{Name: c.Name, URL: c.CareersURL}
		s := scrape.NewJobScraper(comp, m.http)

		// Add a per-company timeout if you like:
		cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		jobs, err := s.GetJobs(cctx, m.prefs)
		cancel()

		if err != nil {
			m.appendWarn(fmt.Sprintf("%s: %v", c.Name, err))
		}

		newForCompany := 0
		for _, sj := range jobs {
			j := models.Job{
				CompanyID:   c.ID,
				Title:       strings.TrimSpace(sj.Title),
				URL:         strings.TrimSpace(sj.URL),
				Location:    strings.TrimSpace(sj.Location),
				Description: strings.TrimSpace(sj.Description),
			}
			if j.URL == "" || j.Title == "" {
				continue
			}
			if err := m.repo.UpsertJob(ctx, &j); err != nil {
				m.appendWarn(fmt.Sprintf("%s: upsert %q failed: %v", c.Name, j.URL, err))
				continue
			}
			newForCompany++
		}
		found += newForCompany

		m.setProgress(((i + 1) * 100 / total), found)
		log.Printf("[SCAN] %-22s jobs=%d (%d/%d, %d%%)", c.Name, newForCompany, i+1, total, m.Status().Percent)
	}

	m.finishOK(100, found)
}

func (m *Manager) setTotal(total int) {
	m.mu.Lock()
	m.state.Total = total
	m.mu.Unlock()
}

func (m *Manager) setProgress(percent, found int) {
	m.mu.Lock()
	m.state.Percent = percent
	m.state.Found = found
	m.mu.Unlock()
}

func (m *Manager) appendWarn(e string) {
	if strings.TrimSpace(e) == "" {
		return
	}
	m.mu.Lock()
	// Keep last message visible; you can aggregate if you prefer.
	m.state.Error = e
	m.mu.Unlock()
}

func (m *Manager) finishWithErr(err error) {
	m.mu.Lock()
	m.state.Running = false
	m.state.Error = err.Error()
	m.mu.Unlock()
}

func (m *Manager) finishOK(pct, found int) {
	m.mu.Lock()
	m.state.Running = false
	m.state.Percent = pct
	m.state.Found = found
	m.mu.Unlock()
}

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
	"jf/internal/models"
	"jf/internal/pool"
	"jf/internal/repo"
	"jf/internal/scrape"
)

type Manager struct {
	repo *repo.SQLiteRepo
	cfg  *config.Config
	http scrape.Doer
	bp   *pool.BrowserPool
	wp   *pool.WorkerPool

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

// NewManager wires both pools. If you don’t need one of them yet, you can pass nil;
// it’ll still work (we gate usage accordingly).
func NewManager(r *repo.SQLiteRepo, cfg *config.Config, httpDoer scrape.Doer, bp *pool.BrowserPool, wp *pool.WorkerPool) *Manager {
	return &Manager{repo: r, cfg: cfg, http: httpDoer, bp: bp, wp: wp}
}

func (m *Manager) Status() ScanState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Stop cancels an active scan (idempotent).
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Manager) StartScan() error {
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

	var wg sync.WaitGroup
	wg.Add(total)

	var found int64 // total jobs
	var done int64  // companies finished

	for _, c := range companies {
		c := c
		runOne := func() {
			defer wg.Done()

			cctx, cancel := context.WithTimeout(ctx, 300*time.Second)
			defer cancel()

			s := scrape.NewJobScraper(c, m.http, m.bp)

			jobs, err := s.GetJobs(cctx, m.cfg)
			if err != nil && cctx.Err() == nil {
				m.appendWarn(fmt.Sprintf("%s: %v", c.Name, err))
			}

			newN := 0
			for _, sj := range jobs {
				select {
				case <-cctx.Done():
					m.appendWarn(fmt.Sprintf("%s: cancelled: %v", c.Name, cctx.Err()))
					break
				default:
				}
				j := models.Job{
					CompanyID:   c.ID,
					Title:       strings.TrimSpace(sj.Title),
					URL:         strings.TrimSpace(sj.URL),
					Location:    strings.TrimSpace(sj.Location),
					Description: strings.TrimSpace(sj.Description),
				}
				//if j.URL == "" || j.Title == "" {
				//	continue
				//}
				if err := m.repo.UpsertJob(cctx, &j); err != nil {
					m.appendWarn(fmt.Sprintf("%s: upsert %q failed: %v", c.Name, j.URL, err))
					continue
				}
				newN++
			}

			totalFound := atomic.AddInt64(&found, int64(newN))
			curDone := int(atomic.AddInt64(&done, 1))
			pct := (curDone * 100) / total
			m.setProgress(pct, int(totalFound))
			log.Printf("[SCAN] %-22s jobs=%d (%d/%d, %d%%)", c.Name, newN, curDone, total, pct)
		}

		if m.wp != nil {
			m.wp.Submit(runOne) // blocks when queue full → backpressure
		} else {
			runOne() // sequential fallback
		}
	}

	wg.Wait()

	select {
	case <-ctx.Done():
		m.finishWithErr(ctx.Err())
	default:
		m.finishOK(100, int(atomic.LoadInt64(&found)))
	}
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

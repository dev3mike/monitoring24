package urlcheck

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/masoudx/monitoring24/internal/storage"
)

// Result is the outcome of a single URL check.
type Result struct {
	CheckID    int64
	CheckedAt  time.Time
	Up         bool
	StatusCode *int
	LatencyMS  *int
	Error      *string
	// Metadata for SSE broadcast
	URL   string
	Label string
}

// Summary aggregates a URL check with its latest result and uptime.
type Summary struct {
	Check      storage.URLCheck
	LastResult *Result
	UptimePct  float64
	History    []Result
}

type checkState struct {
	check  storage.URLCheck
	stopCh chan struct{}
}

// Checker runs per-URL health check goroutines and maintains a result cache.
type Checker struct {
	db       *storage.DB
	client   *http.Client
	mu       sync.RWMutex
	checks   map[int64]*checkState
	summaries map[int64]*Summary
	ResultCh chan Result
}

func NewChecker(db *storage.DB) *Checker {
	transport := &http.Transport{
		MaxIdleConns:        50,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return &Checker{
		db:        db,
		client:    client,
		checks:    make(map[int64]*checkState),
		summaries: make(map[int64]*Summary),
		ResultCh:  make(chan Result, 64),
	}
}

// Start loads existing checks from the DB and begins their goroutines.
func (c *Checker) Start(ctx context.Context) error {
	checks, err := c.db.ListURLChecks()
	if err != nil {
		return err
	}
	for _, ch := range checks {
		if ch.Enabled {
			c.startCheck(ctx, ch)
		}
	}
	return nil
}

func (c *Checker) startCheck(ctx context.Context, ch storage.URLCheck) {
	state := &checkState{
		check:  ch,
		stopCh: make(chan struct{}),
	}
	c.mu.Lock()
	c.checks[ch.ID] = state
	if c.summaries[ch.ID] == nil {
		c.summaries[ch.ID] = &Summary{Check: ch}
	}
	c.mu.Unlock()

	go c.runCheck(ctx, state)
}

func (c *Checker) runCheck(ctx context.Context, state *checkState) {
	interval := time.Duration(state.check.IntervalSeconds) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	c.doAndStore(ctx, state.check)

	for {
		select {
		case <-ticker.C:
			c.doAndStore(ctx, state.check)
		case <-state.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (c *Checker) doAndStore(ctx context.Context, ch storage.URLCheck) {
	result := c.doCheck(ctx, ch)
	result.URL = ch.URL
	result.Label = ch.Label

	sr := storage.URLResult{
		CheckID:    result.CheckID,
		CheckedAt:  result.CheckedAt,
		Up:         result.Up,
		StatusCode: result.StatusCode,
		LatencyMS:  result.LatencyMS,
		Error:      result.Error,
	}
	_ = c.db.InsertURLResult(sr)

	uptime, _ := c.db.URLUptime(ch.ID, time.Now().Add(-24*time.Hour))

	c.mu.Lock()
	if s, ok := c.summaries[ch.ID]; ok {
		s.LastResult = &result
		s.UptimePct = uptime
		// keep last 20 in memory
		s.History = append([]Result{result}, s.History...)
		if len(s.History) > 20 {
			s.History = s.History[:20]
		}
	}
	c.mu.Unlock()

	select {
	case c.ResultCh <- result:
	default:
	}
}

func (c *Checker) doCheck(ctx context.Context, ch storage.URLCheck) Result {
	timeout := time.Duration(ch.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, ch.URL, nil)
	if err != nil {
		errStr := err.Error()
		return Result{CheckID: ch.ID, CheckedAt: start, Up: false, Error: &errStr}
	}
	req.Header.Set("User-Agent", "monitoring24/1.0 health-check")

	resp, err := c.client.Do(req)
	latencyMs := int(time.Since(start).Milliseconds())
	if err != nil {
		errStr := err.Error()
		return Result{CheckID: ch.ID, CheckedAt: start, Up: false, Error: &errStr}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	code := resp.StatusCode
	up := code < 400
	return Result{
		CheckID:    ch.ID,
		CheckedAt:  start,
		Up:         up,
		StatusCode: &code,
		LatencyMS:  &latencyMs,
	}
}

// Add creates a new URL check in DB and starts its goroutine.
func (c *Checker) Add(ctx context.Context, ch storage.URLCheck) (storage.URLCheck, error) {
	id, err := c.db.InsertURLCheck(ch)
	if err != nil {
		return ch, err
	}
	ch.ID = id

	c.mu.Lock()
	c.summaries[id] = &Summary{Check: ch}
	c.mu.Unlock()

	if ch.Enabled {
		c.startCheck(ctx, ch)
	}
	return ch, nil
}

// Update modifies a URL check and restarts its goroutine.
func (c *Checker) Update(ctx context.Context, ch storage.URLCheck) error {
	if err := c.db.UpdateURLCheck(ch); err != nil {
		return err
	}
	c.stopCheck(ch.ID)

	c.mu.Lock()
	if s, ok := c.summaries[ch.ID]; ok {
		s.Check = ch
	}
	c.mu.Unlock()

	if ch.Enabled {
		c.startCheck(ctx, ch)
	}
	return nil
}

// Remove stops a URL check goroutine and deletes it from DB.
func (c *Checker) Remove(id int64) error {
	c.stopCheck(id)
	c.mu.Lock()
	delete(c.summaries, id)
	c.mu.Unlock()
	return c.db.DeleteURLCheck(id)
}

func (c *Checker) stopCheck(id int64) {
	c.mu.Lock()
	state, ok := c.checks[id]
	if ok {
		close(state.stopCh)
		delete(c.checks, id)
	}
	c.mu.Unlock()
}

// Summaries returns a snapshot of all current check summaries.
func (c *Checker) Summaries() []Summary {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Summary, 0, len(c.summaries))
	for _, s := range c.summaries {
		cp := *s
		out = append(out, cp)
	}
	return out
}

// GetSummary returns the summary for a specific check.
func (c *Checker) GetSummary(id int64) (*Summary, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.summaries[id]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

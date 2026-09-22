package ghsnapshot

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type address struct {
	InstallationID int64
	Owner          string
	Repo           string
	Number         int32
}

var (
	defaultChaseBackoff  = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}
	defaultConcurrency   = 12
	defaultViewTTL       = 60 * time.Second
	defaultSweepTTL      = 10 * time.Minute
	defaultSweepInterval = 10 * time.Minute
	defaultSweepMaxRows  = int32(200)
	maxChaseAttempts     = 12
	queueBuffer          = 2048
)

type Manager struct {
	client    *Client
	queries   *db.Queries
	pool      TxBeginner
	onApplied func(ctx context.Context, prID pgtype.UUID)

	concurrency   int
	viewTTL       time.Duration
	sweepTTL      time.Duration
	sweepInterval time.Duration
	sweepMaxRows  int32
	chaseBackoff  []time.Duration
	now           func() time.Time
	jitter        func() time.Duration

	fetch func(ctx context.Context, c *Client, installationID int64, owner, repo string, number int32) (*PRSnapshot, error)

	queue chan address

	mu       sync.Mutex
	active   map[address]bool
	inFlight map[address]bool
	trailing map[address]bool
	attempts map[address]int

	sweepAfter address

	rateUntil map[int64]time.Time

	ctx     context.Context
	started bool
}

func NewManager(client *Client, queries *db.Queries, pool TxBeginner, onApplied func(ctx context.Context, prID pgtype.UUID)) *Manager {
	return &Manager{
		client:        client,
		queries:       queries,
		pool:          pool,
		onApplied:     onApplied,
		concurrency:   defaultConcurrency,
		viewTTL:       defaultViewTTL,
		sweepTTL:      defaultSweepTTL,
		sweepInterval: defaultSweepInterval,
		sweepMaxRows:  defaultSweepMaxRows,
		chaseBackoff:  defaultChaseBackoff,
		now:           time.Now,
		jitter:        func() time.Duration { return time.Duration(rand.Int63n(int64(250 * time.Millisecond))) },
		fetch:         FetchPRSnapshot,
		queue:         make(chan address, queueBuffer),
		active:        map[address]bool{},
		inFlight:      map[address]bool{},
		trailing:      map[address]bool{},
		attempts:      map[address]int{},
		rateUntil:     map[int64]time.Time{},
	}
}

func (m *Manager) Enabled() bool { return m != nil && m.client.Enabled() }

func (m *Manager) Start(ctx context.Context) {
	if !m.Enabled() {
		return
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.ctx = ctx
	m.mu.Unlock()

	for i := 0; i < m.concurrency; i++ {
		go m.worker(ctx)
	}
	go m.sweepLoop(ctx)
}

func (m *Manager) Enqueue(installationID int64, owner, repo string, number int32) {
	if !m.Enabled() {
		return
	}
	addr := address{InstallationID: installationID, Owner: owner, Repo: repo, Number: number}
	m.mu.Lock()
	if m.active[addr] {
		if m.inFlight[addr] {
			m.trailing[addr] = true
		}
		m.mu.Unlock()
		return
	}
	m.active[addr] = true
	m.mu.Unlock()

	select {
	case m.queue <- addr:
	default:

		m.mu.Lock()
		delete(m.active, addr)
		delete(m.inFlight, addr)
		delete(m.trailing, addr)
		m.mu.Unlock()
		slog.Warn("ghsnapshot: refresh queue full, dropping enqueue")
	}
}

func (m *Manager) MaybeEnqueueOnView(installationID int64, owner, repo string, number int32, fetchedAt time.Time, hasFetched bool) {
	if !m.Enabled() {
		return
	}
	if hasFetched && m.now().Sub(fetchedAt) < m.viewTTL {
		return
	}
	m.Enqueue(installationID, owner, repo, number)
}

func (m *Manager) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case addr := <-m.queue:

			if pause := m.rateLimitPause(addr.InstallationID); pause > 0 {
				m.deferActive(ctx, addr, pause)
				continue
			}
			m.mu.Lock()
			m.inFlight[addr] = true
			m.mu.Unlock()
			m.process(ctx, addr)
			m.finish(addr)
		}
	}
}

func (m *Manager) process(ctx context.Context, addr address) {

	if j := m.jitter(); j > 0 {
		if !sleepCtx(ctx, j) {
			return
		}
	}

	snap, err := m.fetch(ctx, m.client, addr.InstallationID, addr.Owner, addr.Repo, addr.Number)
	if err != nil {
		var rl *RateLimitError
		if asRateLimit(err, &rl) {
			m.extendRateLimit(addr.InstallationID, rl.RetryAfter)

			return
		}

		slog.Warn("ghsnapshot: fetch failed", "owner", addr.Owner, "repo", addr.Repo, "number", addr.Number, "err", err.Error())
		return
	}

	rows, err := m.queries.ListGitHubPRRowsByAddress(ctx, db.ListGitHubPRRowsByAddressParams{
		InstallationID: addr.InstallationID,
		RepoOwner:      addr.Owner,
		RepoName:       addr.Repo,
		PrNumber:       addr.Number,
	})
	if err != nil {
		slog.Warn("ghsnapshot: list rows failed", "err", err.Error())
		return
	}

	anyApplied := false
	anyOpenApplied := false
	for _, row := range rows {
		applied, err := m.applySnapshot(ctx, row.ID, snap)
		if err != nil {
			slog.Warn("ghsnapshot: apply snapshot failed", "err", err.Error())
			continue
		}
		if !applied {
			continue
		}
		anyApplied = true
		if row.State == "open" || row.State == "draft" {
			anyOpenApplied = true
		}
		if m.onApplied != nil {
			m.onApplied(ctx, row.ID)
		}
	}

	if anyApplied && anyOpenApplied && !snap.Decided() {
		m.scheduleChase(addr)
	} else {
		m.mu.Lock()
		delete(m.attempts, addr)
		m.mu.Unlock()
	}
}

func (m *Manager) rateLimitPause(installationID int64) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	until, ok := m.rateUntil[installationID]
	if !ok {
		return 0
	}
	pause := until.Sub(m.now())
	if pause <= 0 {
		delete(m.rateUntil, installationID)
		return 0
	}
	return pause
}

func (m *Manager) extendRateLimit(installationID int64, retryAfter time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	until := m.now().Add(retryAfter)
	if until.After(m.rateUntil[installationID]) {
		m.rateUntil[installationID] = until
	}
}

func (m *Manager) deferActive(ctx context.Context, addr address, delay time.Duration) {
	time.AfterFunc(delay, func() {
		if ctx.Err() != nil {
			m.release(addr)
			return
		}
		select {
		case m.queue <- addr:
		default:
			m.release(addr)
			slog.Warn("ghsnapshot: refresh queue full, dropping rate-limited enqueue")
		}
	})
}

func (m *Manager) release(addr address) {
	m.mu.Lock()
	delete(m.active, addr)
	delete(m.inFlight, addr)
	delete(m.trailing, addr)
	m.mu.Unlock()
}

func (m *Manager) finish(addr address) {
	m.mu.Lock()
	if !m.trailing[addr] {
		delete(m.active, addr)
		delete(m.inFlight, addr)
		m.mu.Unlock()
		return
	}
	delete(m.trailing, addr)
	delete(m.inFlight, addr)
	m.mu.Unlock()

	select {
	case m.queue <- addr:
	default:

		m.mu.Lock()
		delete(m.active, addr)
		delete(m.inFlight, addr)
		delete(m.trailing, addr)
		m.mu.Unlock()
		slog.Warn("ghsnapshot: refresh queue full, dropping trailing enqueue")
	}
}

func (m *Manager) applySnapshot(ctx context.Context, prID pgtype.UUID, snap *PRSnapshot) (bool, error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	q := m.queries.WithTx(tx)

	rollup := pgtype.Text{}
	if snap.HasChecks {
		rollup = textOrNull(snap.RollupState)
	}
	n, err := q.UpdateGitHubPRSnapshot(ctx, db.UpdateGitHubPRSnapshotParams{
		ApiMergeable:        textOrNull(snap.Mergeable),
		ApiMergeStateStatus: textOrNull(snap.MergeStateStatus),
		ChecksRollupState:   rollup,
		HeadSha:             snap.HeadSHA,
		FetchedAt:           tsFromTime(m.now()),
		PrID:                prID,
	})
	if err != nil {
		return false, err
	}
	if n == 0 {

		return false, nil
	}
	if err := q.DeleteGitHubPRCheckRuns(ctx, prID); err != nil {
		return false, err
	}
	for i, c := range snap.Contexts {
		if err := q.InsertGitHubPRCheckRun(ctx, db.InsertGitHubPRCheckRunParams{
			PrID:            prID,
			HeadSha:         snap.HeadSHA,
			Ordinal:         int32(i),
			Name:            c.Name,
			Status:          c.Status,
			Conclusion:      textOrNull(c.Conclusion),
			DetailsUrl:      textOrNull(c.DetailsURL),
			IsStatusContext: c.IsStatusContext,
		}); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Manager) scheduleChase(addr address) {
	m.mu.Lock()
	attempt := m.attempts[addr]
	if attempt >= maxChaseAttempts {
		delete(m.attempts, addr)
		m.mu.Unlock()
		return
	}
	m.attempts[addr] = attempt + 1
	idx := attempt
	if idx >= len(m.chaseBackoff) {
		idx = len(m.chaseBackoff) - 1
	}
	delay := m.chaseBackoff[idx]
	m.mu.Unlock()
	m.scheduleRetry(addr, delay)
}

func (m *Manager) scheduleRetry(addr address, delay time.Duration) {
	ctx := m.ctx
	time.AfterFunc(delay, func() {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		m.Enqueue(addr.InstallationID, addr.Owner, addr.Repo, addr.Number)
	})
}

func (m *Manager) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(m.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweepOnce(ctx)
		}
	}
}

func (m *Manager) sweepOnce(ctx context.Context) {
	m.mu.Lock()
	after := m.sweepAfter
	m.mu.Unlock()

	rows, err := m.queries.ListStaleUndecidedGitHubPRs(ctx, db.ListStaleUndecidedGitHubPRsParams{
		OlderThan:           tsFromTime(m.now().Add(-m.sweepTTL)),
		AfterInstallationID: after.InstallationID,
		AfterRepoOwner:      after.Owner,
		AfterRepoName:       after.Repo,
		AfterPrNumber:       after.Number,
		MaxRows:             m.sweepMaxRows,
	})
	if err != nil {
		slog.Warn("ghsnapshot: sweep query failed", "err", err.Error())
		return
	}
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		m.mu.Lock()
		m.sweepAfter = address{
			InstallationID: last.InstallationID,
			Owner:          last.RepoOwner,
			Repo:           last.RepoName,
			Number:         last.PrNumber,
		}
		m.mu.Unlock()
	}
	for _, r := range rows {
		m.Enqueue(r.InstallationID, r.RepoOwner, r.RepoName, r.PrNumber)
	}
}

func asRateLimit(err error, target **RateLimitError) bool {
	if rl, ok := err.(*RateLimitError); ok {
		*target = rl
		return true
	}
	return false
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func tsFromTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

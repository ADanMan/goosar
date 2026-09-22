package engine

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/integrations/channel"
	"github.com/adanman/goosar/server/internal/util"
)

type Installation struct {
	ID pgtype.UUID

	ChannelType channel.Type

	Fingerprint string

	Config json.RawMessage
}

type AcquireLeaseParams struct {
	ID        pgtype.UUID
	Token     string
	ExpiresAt time.Time
}

type ReleaseLeaseParams struct {
	ID    pgtype.UUID
	Token string
}

var ErrLeaseNotAcquired = errors.New("engine: ws lease held elsewhere")

type InstallationStore interface {
	ListActiveInstallations(ctx context.Context) ([]Installation, error)

	AcquireWSLease(ctx context.Context, arg AcquireLeaseParams) error

	ReleaseWSLease(ctx context.Context, arg ReleaseLeaseParams) error
}

type Config struct {
	LeaseTTL time.Duration

	LeaseRenewInterval time.Duration

	PollInterval time.Duration

	MinBackoff        time.Duration
	MaxBackoff        time.Duration
	ResetBackoffAfter time.Duration

	LeaseReleaseTimeout time.Duration

	DisconnectTimeout time.Duration

	ShutdownTimeout time.Duration

	Now func() time.Time

	Logger *slog.Logger
}

func (c Config) withDefaults() Config {
	if c.LeaseTTL == 0 {
		c.LeaseTTL = 90 * time.Second
	}
	if c.LeaseRenewInterval == 0 {
		c.LeaseRenewInterval = 30 * time.Second
	}
	if c.PollInterval == 0 {
		c.PollInterval = 30 * time.Second
	}
	if c.MinBackoff == 0 {
		c.MinBackoff = 2 * time.Second
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 60 * time.Second
	}
	if c.ResetBackoffAfter == 0 {
		c.ResetBackoffAfter = 60 * time.Second
	}
	if c.LeaseReleaseTimeout == 0 {
		c.LeaseReleaseTimeout = 5 * time.Second
	}
	if c.DisconnectTimeout == 0 {
		c.DisconnectTimeout = 5 * time.Second
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 15 * time.Second
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

type Supervisor struct {
	store    InstallationStore
	registry *channel.Registry
	handler  channel.InboundHandler
	cfg      Config

	nodeID string

	mu sync.Mutex

	supervisors map[string]supervisorEntry

	supervisorGen uint64
	wg            sync.WaitGroup
	stopped       bool
	stopChan      chan struct{}
}

type supervisorEntry struct {
	cancel      context.CancelFunc
	fingerprint string
	gen         uint64
}

func NewSupervisor(store InstallationStore, registry *channel.Registry, handler channel.InboundHandler, cfg Config) *Supervisor {
	cfg = cfg.withDefaults()
	return &Supervisor{
		store:       store,
		registry:    registry,
		handler:     handler,
		cfg:         cfg,
		nodeID:      newNodeID(),
		supervisors: make(map[string]supervisorEntry),
		stopChan:    make(chan struct{}),
	}
}

func (s *Supervisor) NodeID() string { return s.nodeID }

func (s *Supervisor) Run(ctx context.Context) {
	defer close(s.stopChan)

	s.sweep(ctx)

	t := time.NewTicker(s.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.cancelAll()
			return
		case <-t.C:
			s.sweep(ctx)
		}
	}
}

func (s *Supervisor) Wait() {
	<-s.stopChan
	s.wg.Wait()
}

func (s *Supervisor) WaitWithTimeout(timeout time.Duration) bool {
	if timeout <= 0 {
		s.Wait()
		return true
	}
	done := make(chan struct{})
	go func() {
		s.Wait()
		close(done)
	}()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		return false
	}
}

func (s *Supervisor) ShutdownTimeout() time.Duration { return s.cfg.ShutdownTimeout }

func (s *Supervisor) sweep(ctx context.Context) {
	rows, err := s.store.ListActiveInstallations(ctx)
	if err != nil {
		s.cfg.Logger.Warn("channel engine: list active installations failed", "error", err)
		return
	}
	active := make(map[string]struct{}, len(rows))
	for _, row := range rows {

		if _, ok := s.registry.Lookup(row.ChannelType); !ok {
			continue
		}
		id := uuidString(row.ID)
		active[id] = struct{}{}
		s.maybeRestartOnRotation(id, row)
		s.startSupervisor(ctx, row)
	}

	s.mu.Lock()
	for id, entry := range s.supervisors {
		if _, stillActive := active[id]; !stillActive {
			entry.cancel()
			delete(s.supervisors, id)
		}
	}
	s.mu.Unlock()
}

func (s *Supervisor) maybeRestartOnRotation(id string, row Installation) {
	want := row.Fingerprint
	s.mu.Lock()
	entry, ok := s.supervisors[id]
	if !ok || entry.fingerprint == want {
		s.mu.Unlock()
		return
	}
	s.cfg.Logger.Info("channel engine: credentials rotated, restarting supervisor",
		"installation_id", id,
		"channel_type", string(row.ChannelType),
	)
	entry.cancel()
	delete(s.supervisors, id)
	s.mu.Unlock()
}

func (s *Supervisor) startSupervisor(parent context.Context, inst Installation) {
	id := uuidString(inst.ID)
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if _, exists := s.supervisors[id]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.supervisorGen++
	gen := s.supervisorGen
	s.supervisors[id] = supervisorEntry{
		cancel:      cancel,
		fingerprint: inst.Fingerprint,
		gen:         gen,
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go s.supervise(ctx, inst, id, gen)
}

func leaseToken(nodeID string, gen uint64) string {
	return nodeID + "-g" + strconv.FormatUint(gen, 10)
}

func (s *Supervisor) supervise(ctx context.Context, inst Installation, id string, gen uint64) {
	defer s.wg.Done()
	defer func() {

		s.mu.Lock()
		if entry, ok := s.supervisors[id]; ok && entry.gen == gen {
			delete(s.supervisors, id)
		}
		s.mu.Unlock()
	}()

	leaseTok := leaseToken(s.nodeID, gen)
	log := s.cfg.Logger.With(
		"installation_id", id,
		"channel_type", string(inst.ChannelType),
		"node_id", s.nodeID,
		"lease_token", leaseTok,
	)
	backoff := s.cfg.MinBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		leased, err := s.acquireLease(ctx, inst.ID, leaseTok)
		if err != nil {
			log.Warn("channel engine: acquire lease error", "error", err)
			if sleep(ctx, s.cfg.LeaseRenewInterval) {
				return
			}
			continue
		}
		if !leased {
			if sleep(ctx, s.cfg.LeaseRenewInterval) {
				return
			}
			continue
		}

		ch, err := s.registry.Build(channel.Config{
			Type:    inst.ChannelType,
			Raw:     inst.Config,
			Handler: s.handler,
		})
		if err != nil {
			log.Error("channel engine: build channel failed", "error", err)
			s.releaseLease(inst.ID, leaseTok)
			if sleep(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, s.cfg.MaxBackoff)
			continue
		}

		runCtx, runCancel := context.WithCancel(ctx)
		renewDone := make(chan struct{})
		go func() {
			defer close(renewDone)

			s.renewLeaseUntil(runCtx, runCancel, inst.ID, leaseTok)
		}()

		startedAt := s.cfg.Now()
		runErr := ch.Connect(runCtx)
		runCancel()
		<-renewDone
		s.disconnect(ch, id, log)
		s.releaseLease(inst.ID, leaseTok)

		if ctx.Err() != nil {
			return
		}

		uptime := s.cfg.Now().Sub(startedAt)
		if uptime >= s.cfg.ResetBackoffAfter {
			backoff = s.cfg.MinBackoff
		}
		if runErr != nil {
			log.Warn("channel engine: connection exited with error", "error", runErr, "uptime", uptime.String())
		} else {
			log.Info("channel engine: connection exited cleanly", "uptime", uptime.String())
		}
		if sleep(ctx, jitter(backoff)) {
			return
		}
		backoff = nextBackoff(backoff, s.cfg.MaxBackoff)
	}
}

func (s *Supervisor) acquireLease(ctx context.Context, instID pgtype.UUID, token string) (bool, error) {
	expires := s.cfg.Now().Add(s.cfg.LeaseTTL)
	err := s.store.AcquireWSLease(ctx, AcquireLeaseParams{
		ID:        instID,
		Token:     token,
		ExpiresAt: expires,
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrLeaseNotAcquired) {
		return false, nil
	}
	return false, err
}

func (s *Supervisor) renewLeaseUntil(ctx context.Context, cancelRun context.CancelFunc, instID pgtype.UUID, token string) {
	t := time.NewTicker(s.cfg.LeaseRenewInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			leased, err := s.acquireLease(ctx, instID, token)
			if err != nil {
				s.cfg.Logger.Warn("channel engine: lease renewal error",
					"installation_id", uuidString(instID),
					"error", err,
				)
				continue
			}
			if !leased {
				s.cfg.Logger.Warn("channel engine: lease lost; tearing down connection",
					"installation_id", uuidString(instID),
				)
				cancelRun()
				return
			}
		}
	}
}

func (s *Supervisor) releaseLease(instID pgtype.UUID, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.LeaseReleaseTimeout)
	defer cancel()
	if err := s.store.ReleaseWSLease(ctx, ReleaseLeaseParams{
		ID:    instID,
		Token: token,
	}); err != nil {
		s.cfg.Logger.Warn("channel engine: release lease failed",
			"installation_id", uuidString(instID),
			"error", err,
		)
	}
}

func (s *Supervisor) disconnect(ch channel.Channel, id string, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.DisconnectTimeout)
	defer cancel()
	if err := ch.Disconnect(ctx); err != nil {
		log.Warn("channel engine: disconnect failed", "installation_id", id, "error", err)
	}
}

func (s *Supervisor) cancelAll() {
	s.mu.Lock()
	s.stopped = true
	for id, entry := range s.supervisors {
		entry.cancel()
		delete(s.supervisors, id)
	}
	s.mu.Unlock()
}

func newNodeID() string {
	buf := make([]byte, 16)
	if _, err := cryptorand.Read(buf); err != nil {

		return fmt.Sprintf("nodeid-fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	delta := d / 2
	return d - delta + time.Duration(mathrand.Int64N(int64(2*delta)+1))
}

func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() != nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
	}
}

func uuidString(u pgtype.UUID) string { return util.UUIDToString(u) }

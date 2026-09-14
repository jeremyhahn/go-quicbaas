// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package lease provides lease management for time-limited resources.
package lease

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// =============================================================================
// WHERE LEASE STATE LIVES
//
// In the embedded qrdb node, reached through interfaces.KVStore — the single
// storage contract every quicbaas component consumes (pkg/orchestration's
// stores, pkg/iam's backends and the scheduler's storage all sit on it). In
// BaaS mode that interface is satisfied by pkg/server's qrdb-SDK-backed store,
// so a lease is durable, replicated and barrier-encrypted at rest for free; in
// a unit test it is satisfied by kvstore.NewMemory(), which is qrdb's own
// in-memory engine through the SDK re-export.
//
// It is deliberately NOT pkg/storage.Storage. That interface describes a LOCAL
// engine — batches, snapshots, bidirectional iterators — and BaaS mode has no
// instance of one to hand a lease manager (pkg/server/lifecycle.go said so
// explicitly, which is why nothing ever constructed this type in production).
// Depending on the six-verb KV instead is what makes the wiring possible at
// all, and it costs nothing: a lease manager only ever does point reads, point
// writes and one prefix scan.
// =============================================================================

// LeaseManager manages the lifecycle of leases for dynamic secrets and credentials.
// It handles creation, renewal, revocation, and automatic expiration of leases.
type LeaseManager interface {
	// CreateLease creates a new lease for the specified path with the given TTL.
	CreateLease(ctx context.Context, path string, ttl time.Duration, opts *LeaseOptions) (*Lease, error)

	// RenewLease extends the expiration time of an existing lease.
	RenewLease(ctx context.Context, leaseID string, increment time.Duration) (*Lease, error)

	// GetLease retrieves a lease by its ID, whatever state it is in. Use
	// Validate to ask whether the grant behind it is still good.
	GetLease(ctx context.Context, leaseID string) (*Lease, error)

	// Validate returns the lease only while it is ACTIVE, and otherwise
	// reports exactly why it is not: LeaseNotFoundError, LeaseRevokedError or
	// LeaseExpiredError. It is the enforcement point every authorization
	// decision made on a lease ID must route through.
	Validate(ctx context.Context, leaseID string) (*Lease, error)

	// RevokeLease revokes a lease, making it immediately invalid. Revoking a
	// lease that is already revoked, or that no longer exists, is a no-op so a
	// retrying caller converges.
	RevokeLease(ctx context.Context, leaseID string) error

	// RevokePrefix revokes all leases matching the specified path prefix.
	RevokePrefix(ctx context.Context, prefix string) (int, error)

	// ListLeases returns all leases matching the specified path prefix,
	// including revoked tombstones that have not yet been purged.
	ListLeases(ctx context.Context, prefix string) ([]*Lease, error)

	// ReapExpired revokes every lease whose TTL has elapsed and purges every
	// tombstone older than the retention window, reporting how many of each.
	ReapExpired(ctx context.Context) (revoked int, purged int, err error)

	// StartExpirationLoop starts a background goroutine that periodically
	// calls ReapExpired.
	StartExpirationLoop(ctx context.Context) error

	// Close gracefully shuts down the lease manager and stops the expiration loop.
	Close() error
}

const (
	// LeasePrefix is the storage key prefix for all leases.
	LeasePrefix = "/_leases/"

	// DefaultExpirationCheckInterval is how often to check for expired leases.
	DefaultExpirationCheckInterval = 10 * time.Second

	// MinLeaseTTL is the minimum allowed lease TTL.
	MinLeaseTTL = 1 * time.Second

	// MaxLeaseTTL is the maximum allowed lease TTL.
	MaxLeaseTTL = 365 * 24 * time.Hour

	// DefaultTombstoneRetention is how long a revoked lease's record is kept
	// before it is purged.
	//
	// It is not zero because a tombstone is the evidence: an operator asking
	// "was this credential still leased when it was used?" gets "revoked at
	// 14:02" rather than "no such lease", and a caller presenting a revoked
	// lease is refused with a reason instead of a shrug. It is not unbounded
	// because every tombstone is a durable key; a day outlives any session the
	// platform runs and bounds the keyspace.
	DefaultTombstoneRetention = 24 * time.Hour
)

// Manager is the concrete implementation of LeaseManager.
type Manager struct {
	store              interfaces.KVStore
	logger             interfaces.Logger
	metrics            *leaseMetrics
	checkInterval      time.Duration
	tombstoneRetention time.Duration

	// Expiration loop control
	expirationCtx     context.Context
	expirationCancel  context.CancelFunc
	expirationRunning bool

	// State management
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
}

// Compile-time interface satisfaction check.
var _ LeaseManager = (*Manager)(nil)

// leaseMetrics tracks lease operations.
type leaseMetrics struct {
	leasesCreated     *metrics.CounterVec
	leasesRenewed     *metrics.CounterVec
	leasesRevoked     *metrics.CounterVec
	leasesExpired     *metrics.Counter
	activeLeasesCount *metrics.Gauge
	leaseOpDuration   *metrics.HistogramVec
}

// ManagerConfig contains configuration for the lease manager.
type ManagerConfig struct {
	// Store is the durable key-value store lease state is persisted in. In
	// BaaS mode this is the embedded qrdb node's KV; in tests it is
	// kvstore.NewMemory().
	Store interfaces.KVStore

	// Logger for operation logging.
	Logger interfaces.Logger

	// ExpirationCheckInterval is how often to check for expired leases.
	// Defaults to DefaultExpirationCheckInterval.
	ExpirationCheckInterval time.Duration

	// TombstoneRetention is how long a revoked lease record is kept before it
	// is purged. Defaults to DefaultTombstoneRetention.
	TombstoneRetention time.Duration
}

// NewManager creates a new lease manager implementation.
func NewManager(config *ManagerConfig) (*Manager, error) {
	if config == nil {
		return nil, errors.InvalidInput("lease.NewManager", "config cannot be nil")
	}

	if config.Store == nil {
		return nil, errors.InvalidInput("lease.NewManager", "store cannot be nil")
	}

	if config.Logger == nil {
		return nil, errors.InvalidInput("lease.NewManager", "logger cannot be nil")
	}

	checkInterval := config.ExpirationCheckInterval
	if checkInterval <= 0 {
		checkInterval = DefaultExpirationCheckInterval
	}

	retention := config.TombstoneRetention
	if retention <= 0 {
		retention = DefaultTombstoneRetention
	}

	m := &Manager{
		store:              config.Store,
		logger:             config.Logger.WithField("component", "lease_manager"),
		checkInterval:      checkInterval,
		tombstoneRetention: retention,
	}

	m.initMetrics()

	m.logger.Info("Lease manager initialized")

	return m, nil
}

// initMetrics initializes Prometheus metrics for lease operations.
func (m *Manager) initMetrics() {
	m.metrics = &leaseMetrics{
		leasesCreated: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "lease",
			Name:      "leases_created_total",
			Help:      "Total number of leases created",
		}, []string{"status"}),

		leasesRenewed: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "lease",
			Name:      "leases_renewed_total",
			Help:      "Total number of lease renewals",
		}, []string{"status"}),

		leasesRevoked: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "lease",
			Name:      "leases_revoked_total",
			Help:      "Total number of leases revoked",
		}, []string{"type"}),

		leasesExpired: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "lease",
			Name:      "leases_expired_total",
			Help:      "Total number of leases expired",
		}),

		activeLeasesCount: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "lease",
			Name:      "active_leases",
			Help:      "Current number of active leases",
		}),

		leaseOpDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "lease",
			Name:      "operation_duration_seconds",
			Help:      "Duration of lease operations",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		}, []string{"operation"}),
	}
}

// CreateLease creates a new lease for the specified path with the given TTL.
func (m *Manager) CreateLease(ctx context.Context, path string, ttl time.Duration, opts *LeaseOptions) (*Lease, error) {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("create").Observe(time.Since(start).Seconds())
	}()

	if err := m.operable("Manager.CreateLease"); err != nil {
		return nil, err
	}

	if path == "" {
		m.metrics.leasesCreated.With("error").Inc()
		return nil, errors.InvalidInput("Manager.CreateLease", "path cannot be empty")
	}

	if ttl < MinLeaseTTL {
		m.metrics.leasesCreated.With("error").Inc()
		return nil, errors.Newf(errors.ErrInvalidInput, "Manager.CreateLease", "TTL must be at least %v", MinLeaseTTL)
	}

	if ttl > MaxLeaseTTL {
		m.metrics.leasesCreated.With("error").Inc()
		return nil, errors.Newf(errors.ErrInvalidInput, "Manager.CreateLease", "TTL cannot exceed %v", MaxLeaseTTL)
	}

	if err := contextErr(ctx, "Manager.CreateLease"); err != nil {
		m.metrics.leasesCreated.With("error").Inc()
		return nil, err
	}

	if opts == nil {
		opts = DefaultLeaseOptions()
	}

	leaseID, err := generateLeaseID()
	if err != nil {
		m.metrics.leasesCreated.With("error").Inc()
		return nil, errors.Wrapf(err, errors.ErrInternal, "Manager.CreateLease", "failed to generate lease ID")
	}

	now := time.Now()
	lease := &Lease{
		LeaseID:     leaseID,
		SecretPath:  path,
		IssuedAt:    now,
		ExpiresAt:   now.Add(ttl),
		TTL:         ttl,
		RenewCount:  0,
		MaxRenewals: opts.MaxRenewals,
		Renewable:   opts.Renewable,
		Metadata:    opts.Metadata,
	}

	if err := m.storeLease(ctx, "Manager.CreateLease", lease); err != nil {
		m.metrics.leasesCreated.With("error").Inc()
		return nil, err
	}

	m.metrics.leasesCreated.With("success").Inc()
	m.metrics.activeLeasesCount.Inc()

	// THE LEASE ID, THE PATH AND THE TTL ARE NOT SECRETS. The material a lease
	// grants access to is never in scope here — a Manager never sees it — so no
	// log line this package writes can carry credential content.
	m.logger.WithFields(map[string]interface{}{
		"lease_id": leaseID,
		"path":     path,
		"ttl":      ttl.String(),
	}).Info("Lease created")

	return lease, nil
}

// RenewLease extends the expiration time of an existing lease.
func (m *Manager) RenewLease(ctx context.Context, leaseID string, increment time.Duration) (*Lease, error) {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("renew").Observe(time.Since(start).Seconds())
	}()

	if err := m.operable("Manager.RenewLease"); err != nil {
		return nil, err
	}

	if leaseID == "" {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, errors.InvalidInput("Manager.RenewLease", "lease ID cannot be empty")
	}

	if err := contextErr(ctx, "Manager.RenewLease"); err != nil {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, err
	}

	lease, err := m.loadLease(ctx, "Manager.RenewLease", leaseID)
	if err != nil {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, err
	}

	// A RENEWAL IS AN AUTHORIZATION DECISION, so it answers in the same
	// vocabulary Validate does rather than with one undifferentiated refusal: a
	// caller must be able to tell "revoked, stop asking" from "expired, ask for
	// a new grant" from "this grant was never renewable".
	if lease.RevokedAt != nil {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, NewLeaseRevokedError("Manager.RenewLease", leaseID, *lease.RevokedAt)
	}
	if lease.State() == LeaseStateExpired {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, NewLeaseExpiredError("Manager.RenewLease", leaseID, lease.ExpiresAt)
	}
	if !lease.CanRenew() {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, errors.Newf(errors.ErrNotAllowed, "Manager.RenewLease",
			"lease cannot be renewed (renewable: %v, renew_count: %d/%d)",
			lease.Renewable, lease.RenewCount, lease.MaxRenewals)
	}

	// Use original TTL if increment is 0
	if increment == 0 {
		increment = lease.TTL
	}

	lease.ExpiresAt = time.Now().Add(increment)
	lease.RenewCount++

	if err := m.storeLease(ctx, "Manager.RenewLease", lease); err != nil {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, err
	}

	m.metrics.leasesRenewed.With("success").Inc()

	m.logger.WithFields(map[string]interface{}{
		"lease_id":    leaseID,
		"renew_count": lease.RenewCount,
		"expires_at":  lease.ExpiresAt,
	}).Info("Lease renewed")

	return lease, nil
}

// GetLease retrieves a lease by its ID, in whatever state it is in.
//
// It is the OBSERVABILITY verb: a revoked or expired lease is returned rather
// than refused, so an operator can read what became of it. Nothing may
// authorize on this result — see Validate.
func (m *Manager) GetLease(ctx context.Context, leaseID string) (*Lease, error) {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("get").Observe(time.Since(start).Seconds())
	}()

	if err := m.operable("Manager.GetLease"); err != nil {
		return nil, err
	}

	if leaseID == "" {
		return nil, errors.InvalidInput("Manager.GetLease", "lease ID cannot be empty")
	}

	if err := contextErr(ctx, "Manager.GetLease"); err != nil {
		return nil, err
	}

	return m.loadLease(ctx, "Manager.GetLease", leaseID)
}

// Validate returns the lease only while it is ACTIVE.
//
// THIS IS THE ENFORCEMENT POINT. A revoked lease is refused from its tombstone
// and an expired one from its own ExpiresAt — neither answer waits for the
// expiration loop, so a TTL is a rule rather than a note. Every caller that
// makes an access decision on a lease ID calls this and nothing else.
func (m *Manager) Validate(ctx context.Context, leaseID string) (*Lease, error) {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("validate").Observe(time.Since(start).Seconds())
	}()

	if err := m.operable("Manager.Validate"); err != nil {
		return nil, err
	}

	if leaseID == "" {
		return nil, errors.InvalidInput("Manager.Validate", "lease ID cannot be empty")
	}

	if err := contextErr(ctx, "Manager.Validate"); err != nil {
		return nil, err
	}

	lease, err := m.loadLease(ctx, "Manager.Validate", leaseID)
	if err != nil {
		return nil, err
	}

	switch lease.State() {
	case LeaseStateRevoked:
		return nil, NewLeaseRevokedError("Manager.Validate", leaseID, *lease.RevokedAt)
	case LeaseStateExpired:
		return nil, NewLeaseExpiredError("Manager.Validate", leaseID, lease.ExpiresAt)
	default:
		return lease, nil
	}
}

// RevokeLease revokes a lease, making it immediately invalid.
//
// THE RECORD IS TOMBSTONED, NOT DELETED. Deleting it made LeaseStateRevoked
// unreachable in production — a revoked lease was indistinguishable from one
// that never existed — and left nothing to audit. The tombstone is what lets
// Validate refuse with a reason; ReapExpired purges it once the retention
// window has passed.
//
// Idempotent in both directions: revoking an already-revoked lease keeps the
// original revocation time, and revoking a lease that is gone succeeds with
// nothing to do. A session conclusion that retries therefore converges instead
// of failing on its second attempt.
func (m *Manager) RevokeLease(ctx context.Context, leaseID string) error {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("revoke").Observe(time.Since(start).Seconds())
	}()

	if err := m.operable("Manager.RevokeLease"); err != nil {
		return err
	}

	if leaseID == "" {
		return errors.InvalidInput("Manager.RevokeLease", "lease ID cannot be empty")
	}

	if err := contextErr(ctx, "Manager.RevokeLease"); err != nil {
		return err
	}

	return m.revoke(ctx, "Manager.RevokeLease", leaseID, "manual")
}

// revoke is the shared tombstone write behind RevokeLease, RevokePrefix and
// the expiration reaper, which differ only in what they call the revocation.
func (m *Manager) revoke(ctx context.Context, op, leaseID, kind string) error {
	lease, err := m.loadLease(ctx, op, leaseID)
	if err != nil {
		if IsLeaseNotFound(err) {
			// Nothing left to take away. A conclusion retried after the
			// tombstone was purged must not be told the revocation failed.
			return nil
		}
		m.metrics.leasesRevoked.With("error").Inc()
		return err
	}

	if lease.RevokedAt != nil {
		return nil
	}

	now := time.Now()
	lease.RevokedAt = &now

	if err := m.storeLease(ctx, op, lease); err != nil {
		m.metrics.leasesRevoked.With("error").Inc()
		return err
	}

	m.metrics.leasesRevoked.With(kind).Inc()
	m.metrics.activeLeasesCount.Dec()

	m.logger.WithFields(map[string]interface{}{
		"lease_id": leaseID,
		"kind":     kind,
	}).Info("Lease revoked")

	return nil
}

// RevokePrefix revokes all leases matching the specified path prefix.
func (m *Manager) RevokePrefix(ctx context.Context, prefix string) (int, error) {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("revoke_prefix").Observe(time.Since(start).Seconds())
	}()

	if err := m.operable("Manager.RevokePrefix"); err != nil {
		return 0, err
	}

	if err := contextErr(ctx, "Manager.RevokePrefix"); err != nil {
		return 0, err
	}

	leases, err := m.ListLeases(ctx, prefix)
	if err != nil {
		return 0, err
	}

	revokedCount := 0
	for _, lease := range leases {
		if lease.RevokedAt != nil {
			continue
		}
		if err := m.revoke(ctx, "Manager.RevokePrefix", lease.LeaseID, "prefix"); err != nil {
			m.logger.WithFields(map[string]interface{}{
				"lease_id": lease.LeaseID,
				"error":    err,
			}).Warn("Failed to revoke lease")
			continue
		}
		revokedCount++
	}

	m.logger.WithFields(map[string]interface{}{
		"prefix":  prefix,
		"revoked": revokedCount,
	}).Info("Leases revoked by prefix")

	return revokedCount, nil
}

// ListLeases returns all leases matching the specified path prefix.
func (m *Manager) ListLeases(ctx context.Context, prefix string) ([]*Lease, error) {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("list").Observe(time.Since(start).Seconds())
	}()

	if err := m.operable("Manager.ListLeases"); err != nil {
		return nil, err
	}

	if err := contextErr(ctx, "Manager.ListLeases"); err != nil {
		return nil, err
	}

	leases := make([]*Lease, 0)
	scanErr := m.store.Scan(ctx, LeasePrefix, func(key string, value []byte) error {
		lease := &Lease{}
		if err := json.Unmarshal(value, lease); err != nil {
			// One undecodable record must not hide every other lease from a
			// listing an operator is reading to decide what to revoke.
			m.logger.WithFields(map[string]interface{}{
				"key":   key,
				"error": err,
			}).Warn("Failed to unmarshal lease")
			return nil
		}
		if prefix != "" && !strings.HasPrefix(lease.SecretPath, prefix) {
			return nil
		}
		leases = append(leases, lease)
		return nil
	})
	if scanErr != nil {
		return nil, NewLeaseStoreError("Manager.ListLeases", "scan", "", scanErr)
	}

	return leases, nil
}

// ReapExpired revokes every lease whose TTL has elapsed and purges every
// tombstone past the retention window.
//
// It is exported and synchronous so the sweep is TESTABLE AS A FUNCTION rather
// than as a race against a ticker: a test drives one pass and asserts the
// outcome, with no sleeping and no timing assumption. StartExpirationLoop is a
// ticker around this and nothing else.
func (m *Manager) ReapExpired(ctx context.Context) (int, int, error) {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("reap").Observe(time.Since(start).Seconds())
	}()

	if err := m.operable("Manager.ReapExpired"); err != nil {
		return 0, 0, err
	}

	leases, err := m.ListLeases(ctx, "")
	if err != nil {
		return 0, 0, err
	}

	now := time.Now()
	revoked, purged := 0, 0

	for _, lease := range leases {
		if lease.RevokedAt != nil {
			if now.Sub(*lease.RevokedAt) < m.tombstoneRetention {
				continue
			}
			if err := m.purgeLease(ctx, "Manager.ReapExpired", lease.LeaseID); err != nil {
				m.logger.WithFields(map[string]interface{}{
					"lease_id": lease.LeaseID,
					"error":    err,
				}).Warn("Failed to purge revoked lease")
				continue
			}
			purged++
			continue
		}

		if lease.State() != LeaseStateExpired {
			continue
		}
		if err := m.revoke(ctx, "Manager.ReapExpired", lease.LeaseID, "expired"); err != nil {
			m.logger.WithFields(map[string]interface{}{
				"lease_id": lease.LeaseID,
				"error":    err,
			}).Warn("Failed to revoke expired lease")
			continue
		}
		revoked++
		m.metrics.leasesExpired.Inc()
	}

	if revoked > 0 || purged > 0 {
		m.logger.WithFields(map[string]interface{}{
			"revoked": revoked,
			"purged":  purged,
		}).Info("Expired leases reaped")
	}

	return revoked, purged, nil
}

// StartExpirationLoop starts a background goroutine that periodically reaps.
func (m *Manager) StartExpirationLoop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.Unavailable("Manager.StartExpirationLoop", "manager is closed")
	}

	if m.expirationRunning {
		return errors.New(errors.ErrAlreadyExists, "Manager.StartExpirationLoop", "expiration loop is already running")
	}

	m.expirationCtx, m.expirationCancel = context.WithCancel(ctx) // #nosec G118 -- expirationCancel stored in Manager, called in Close()
	m.expirationRunning = true

	go m.expirationLoop(m.expirationCtx, m.checkInterval)

	m.logger.Info("Expiration loop started")

	return nil
}

// expirationLoop periodically reaps expired leases and aged-out tombstones.
func (m *Manager) expirationLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if _, _, err := m.ReapExpired(ctx); err != nil {
				m.logger.WithError(err).Warn("Lease expiration pass failed")
			}

		case <-ctx.Done():
			m.logger.Info("Expiration loop stopped")
			return
		}
	}
}

// Close gracefully shuts down the lease manager.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		cancel := m.expirationCancel
		m.mu.Unlock()

		if cancel != nil {
			cancel()
		}

		m.logger.Info("Lease manager closed")
	})

	return nil
}

// operable reports whether the manager may still serve requests.
func (m *Manager) operable(op string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return errors.Unavailable(op, "manager is closed")
	}
	return nil
}

// contextErr converts a cancelled or expired context into the coded timeout
// error every verb reports.
func contextErr(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return &errors.QBError{Code: errors.ErrTimeout, Op: op, Err: err}
	}
	return nil
}

// leaseKey renders the store key one lease is persisted under.
func leaseKey(leaseID string) string {
	return LeasePrefix + leaseID
}

// storeLease persists a lease.
func (m *Manager) storeLease(ctx context.Context, op string, lease *Lease) error {
	data, err := json.Marshal(lease)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInternal, op, "failed to marshal lease")
	}

	if err := m.store.Put(ctx, leaseKey(lease.LeaseID), data); err != nil {
		return NewLeaseStoreError(op, "put", lease.LeaseID, err)
	}

	return nil
}

// loadLease retrieves a lease record, reporting a typed not-found for an absent
// one so callers can branch on the distinction.
func (m *Manager) loadLease(ctx context.Context, op, leaseID string) (*Lease, error) {
	data, err := m.store.Get(ctx, leaseKey(leaseID))
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, NewLeaseNotFoundError(op, leaseID)
		}
		return nil, NewLeaseStoreError(op, "get", leaseID, err)
	}
	if len(data) == 0 {
		return nil, NewLeaseNotFoundError(op, leaseID)
	}

	lease := &Lease{}
	if err := json.Unmarshal(data, lease); err != nil {
		return nil, NewLeaseCorruptError(op, leaseID, err)
	}

	return lease, nil
}

// purgeLease removes a lease record outright. Only the reaper calls it, and
// only for a tombstone that has outlived the retention window.
func (m *Manager) purgeLease(ctx context.Context, op, leaseID string) error {
	if err := m.store.Delete(ctx, leaseKey(leaseID)); err != nil {
		return NewLeaseStoreError(op, "delete", leaseID, err)
	}
	return nil
}

// generateLeaseID generates a cryptographically secure random lease ID.
func generateLeaseID() (string, error) {
	bytes, err := xkms.Rand(16)
	if err != nil {
		return "", err
	}
	return "lease_" + hex.EncodeToString(bytes), nil
}

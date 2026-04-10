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
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// LeaseManager manages the lifecycle of leases for dynamic secrets and credentials.
// It handles creation, renewal, revocation, and automatic expiration of leases.
type LeaseManager interface {
	// CreateLease creates a new lease for the specified path with the given TTL.
	CreateLease(ctx context.Context, path string, ttl time.Duration, opts *LeaseOptions) (*Lease, error)

	// RenewLease extends the expiration time of an existing lease.
	RenewLease(ctx context.Context, leaseID string, increment time.Duration) (*Lease, error)

	// GetLease retrieves a lease by its ID.
	GetLease(ctx context.Context, leaseID string) (*Lease, error)

	// RevokeLease revokes a lease, making it immediately invalid.
	RevokeLease(ctx context.Context, leaseID string) error

	// RevokePrefix revokes all leases matching the specified path prefix.
	RevokePrefix(ctx context.Context, prefix string) (int, error)

	// ListLeases returns all leases matching the specified path prefix.
	ListLeases(ctx context.Context, prefix string) ([]*Lease, error)

	// StartExpirationLoop starts a background goroutine that periodically checks
	// for expired leases and revokes them automatically.
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
)

// Manager is the concrete implementation of LeaseManager.
type Manager struct {
	store   storage.Storage
	logger  interfaces.Logger
	metrics *leaseMetrics

	// Expiration loop control
	expirationCtx     context.Context
	expirationCancel  context.CancelFunc
	expirationRunning bool

	// State management
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
}

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
	// Store is the underlying storage for lease persistence.
	Store storage.Storage

	// Logger for operation logging.
	Logger interfaces.Logger

	// ExpirationCheckInterval is how often to check for expired leases.
	// Defaults to DefaultExpirationCheckInterval.
	ExpirationCheckInterval time.Duration
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

	if config.ExpirationCheckInterval == 0 {
		config.ExpirationCheckInterval = DefaultExpirationCheckInterval
	}

	m := &Manager{
		store:  config.Store,
		logger: config.Logger.WithField("component", "lease_manager"),
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

	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, errors.Unavailable("Manager.CreateLease", "manager is closed")
	}
	m.mu.RUnlock()

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

	if ctx.Err() != nil {
		m.metrics.leasesCreated.With("error").Inc()
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "Manager.CreateLease",
			Err:  ctx.Err(),
		}
	}

	if opts == nil {
		opts = DefaultLeaseOptions()
	}

	// Generate lease ID
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

	// Persist lease
	if err := m.storeLease(lease); err != nil {
		m.metrics.leasesCreated.With("error").Inc()
		return nil, errors.Wrapf(err, errors.ErrInternal, "Manager.CreateLease", "failed to store lease")
	}

	m.metrics.leasesCreated.With("success").Inc()
	m.metrics.activeLeasesCount.Inc()

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

	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, errors.Unavailable("Manager.RenewLease", "manager is closed")
	}
	m.mu.RUnlock()

	if leaseID == "" {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, errors.InvalidInput("Manager.RenewLease", "lease ID cannot be empty")
	}

	if ctx.Err() != nil {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "Manager.RenewLease",
			Err:  ctx.Err(),
		}
	}

	// Retrieve existing lease
	lease, err := m.loadLease(leaseID)
	if err != nil {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, errors.Wrapf(err, errors.ErrNotFound, "Manager.RenewLease", "lease not found")
	}

	// Check if lease can be renewed
	if !lease.CanRenew() {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, errors.Newf(errors.ErrNotAllowed, "Manager.RenewLease",
			"lease cannot be renewed (state: %s, renewable: %v, renew_count: %d/%d)",
			lease.State(), lease.Renewable, lease.RenewCount, lease.MaxRenewals)
	}

	// Use original TTL if increment is 0
	if increment == 0 {
		increment = lease.TTL
	}

	// Update lease
	lease.ExpiresAt = time.Now().Add(increment)
	lease.RenewCount++

	// Persist updated lease
	if err := m.storeLease(lease); err != nil {
		m.metrics.leasesRenewed.With("error").Inc()
		return nil, errors.Wrapf(err, errors.ErrInternal, "Manager.RenewLease", "failed to update lease")
	}

	m.metrics.leasesRenewed.With("success").Inc()

	m.logger.WithFields(map[string]interface{}{
		"lease_id":    leaseID,
		"renew_count": lease.RenewCount,
		"expires_at":  lease.ExpiresAt,
	}).Info("Lease renewed")

	return lease, nil
}

// GetLease retrieves a lease by its ID.
func (m *Manager) GetLease(ctx context.Context, leaseID string) (*Lease, error) {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("get").Observe(time.Since(start).Seconds())
	}()

	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, errors.Unavailable("Manager.GetLease", "manager is closed")
	}
	m.mu.RUnlock()

	if leaseID == "" {
		return nil, errors.InvalidInput("Manager.GetLease", "lease ID cannot be empty")
	}

	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "Manager.GetLease",
			Err:  ctx.Err(),
		}
	}

	lease, err := m.loadLease(leaseID)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrNotFound, "Manager.GetLease", "lease not found")
	}

	return lease, nil
}

// RevokeLease revokes a lease, making it immediately invalid.
func (m *Manager) RevokeLease(ctx context.Context, leaseID string) error {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("revoke").Observe(time.Since(start).Seconds())
	}()

	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return errors.Unavailable("Manager.RevokeLease", "manager is closed")
	}
	m.mu.RUnlock()

	if leaseID == "" {
		return errors.InvalidInput("Manager.RevokeLease", "lease ID cannot be empty")
	}

	if ctx.Err() != nil {
		return &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "Manager.RevokeLease",
			Err:  ctx.Err(),
		}
	}

	// Delete lease from storage
	key := []byte(LeasePrefix + leaseID)
	if err := m.store.Delete(key); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "Manager.RevokeLease", "failed to delete lease")
	}

	m.metrics.leasesRevoked.With("manual").Inc()
	m.metrics.activeLeasesCount.Dec()

	m.logger.WithField("lease_id", leaseID).Info("Lease revoked")

	return nil
}

// RevokePrefix revokes all leases matching the specified path prefix.
func (m *Manager) RevokePrefix(ctx context.Context, prefix string) (int, error) {
	start := time.Now()
	defer func() {
		m.metrics.leaseOpDuration.With("revoke_prefix").Observe(time.Since(start).Seconds())
	}()

	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return 0, errors.Unavailable("Manager.RevokePrefix", "manager is closed")
	}
	m.mu.RUnlock()

	if ctx.Err() != nil {
		return 0, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "Manager.RevokePrefix",
			Err:  ctx.Err(),
		}
	}

	leases, err := m.ListLeases(ctx, prefix)
	if err != nil {
		return 0, err
	}

	revokedCount := 0
	for _, lease := range leases {
		if err := m.RevokeLease(ctx, lease.LeaseID); err != nil {
			m.logger.WithFields(map[string]interface{}{
				"lease_id": lease.LeaseID,
				"error":    err,
			}).Warn("Failed to revoke lease")
		} else {
			revokedCount++
		}
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

	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, errors.Unavailable("Manager.ListLeases", "manager is closed")
	}
	m.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "Manager.ListLeases",
			Err:  ctx.Err(),
		}
	}

	// List all lease keys
	iter := m.store.NewIterator(&storage.IteratorOptions{
		LowerBound: []byte(LeasePrefix),
		UpperBound: []byte(LeasePrefix + "~"),
	})
	defer iter.Close()

	leases := make([]*Lease, 0)
	for iter.Next() {
		lease := &Lease{}
		if err := json.Unmarshal(iter.Value(), lease); err != nil {
			m.logger.WithError(err).Warn("Failed to unmarshal lease")
			continue
		}

		// Filter by path prefix if specified
		if prefix != "" && !strings.HasPrefix(lease.SecretPath, prefix) {
			continue
		}

		leases = append(leases, lease)
	}

	return leases, nil
}

// StartExpirationLoop starts a background goroutine that periodically checks for expired leases.
func (m *Manager) StartExpirationLoop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.Unavailable("Manager.StartExpirationLoop", "manager is closed")
	}

	if m.expirationRunning {
		return errors.New(errors.ErrAlreadyExists, "Manager.StartExpirationLoop", "expiration loop is already running")
	}

	m.expirationCtx, m.expirationCancel = context.WithCancel(ctx) // #nosec G118 -- expirationCancel stored in Manager, called in StopExpirationLoop()
	m.expirationRunning = true

	go m.expirationLoop()

	m.logger.Info("Expiration loop started")

	return nil
}

// expirationLoop periodically checks for and revokes expired leases.
func (m *Manager) expirationLoop() {
	ticker := time.NewTicker(DefaultExpirationCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkExpiredLeases()

		case <-m.expirationCtx.Done():
			m.logger.Info("Expiration loop stopped")
			return
		}
	}
}

// checkExpiredLeases finds and revokes all expired leases.
func (m *Manager) checkExpiredLeases() {
	ctx := context.Background()
	leases, err := m.ListLeases(ctx, "")
	if err != nil {
		m.logger.WithError(err).Warn("Failed to list leases for expiration check")
		return
	}

	expiredCount := 0
	for _, lease := range leases {
		if lease.State() == LeaseStateExpired {
			if err := m.RevokeLease(ctx, lease.LeaseID); err != nil {
				m.logger.WithFields(map[string]interface{}{
					"lease_id": lease.LeaseID,
					"error":    err,
				}).Warn("Failed to revoke expired lease")
			} else {
				expiredCount++
				m.metrics.leasesExpired.Inc()
			}
		}
	}

	if expiredCount > 0 {
		m.logger.WithField("count", expiredCount).Info("Expired leases revoked")
	}
}

// Close gracefully shuts down the lease manager.
func (m *Manager) Close() error {
	var closeErr error

	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()

		// Stop expiration loop
		if m.expirationCancel != nil {
			m.expirationCancel()
		}

		m.logger.Info("Lease manager closed")
	})

	return closeErr
}

// storeLease persists a lease to storage.
func (m *Manager) storeLease(lease *Lease) error {
	data, err := json.Marshal(lease)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "Manager.storeLease", "failed to marshal lease")
	}

	key := []byte(LeasePrefix + lease.LeaseID)
	if err := m.store.Set(key, data); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "Manager.storeLease", "failed to store lease")
	}

	return nil
}

// loadLease retrieves a lease from storage.
func (m *Manager) loadLease(leaseID string) (*Lease, error) {
	key := []byte(LeasePrefix + leaseID)
	data, err := m.store.Get(key)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrNotFound, "Manager.loadLease", "failed to get lease")
	}

	lease := &Lease{}
	if err := json.Unmarshal(data, lease); err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "Manager.loadLease", "failed to unmarshal lease")
	}

	return lease, nil
}

// generateLeaseID generates a cryptographically secure random lease ID.
func generateLeaseID() (string, error) {
	bytes, err := xkms.Rand(16)
	if err != nil {
		return "", err
	}
	return "lease_" + hex.EncodeToString(bytes), nil
}

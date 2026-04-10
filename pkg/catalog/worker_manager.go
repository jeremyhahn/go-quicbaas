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

package catalog

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// WorkerManager manages the lifecycle of all background workers.
type WorkerManager struct {
	workers map[string]Worker
	order   []string // Maintains registration order for deterministic startup/shutdown
	logger  interfaces.Logger
	running atomic.Bool
	closed  atomic.Bool
	mu      sync.RWMutex
}

// NewWorkerManager creates a new worker manager.
func NewWorkerManager(logger interfaces.Logger) *WorkerManager {
	return &WorkerManager{
		workers: make(map[string]Worker),
		order:   make([]string, 0),
		logger:  logger,
	}
}

// Register adds a worker to the manager.
// Returns an error if a worker with the same name already exists.
func (m *WorkerManager) Register(w Worker) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if w == nil {
		return ErrWorkerConfigNil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	name := w.Name()
	if _, exists := m.workers[name]; exists {
		return NewWorkerExistsError(name)
	}

	m.workers[name] = w
	m.order = append(m.order, name)

	if m.logger != nil {
		m.logger.WithField("worker", name).Debug("worker registered")
	}

	return nil
}

// Unregister removes a worker from the manager.
// If the worker is running, it will be stopped first.
func (m *WorkerManager) Unregister(name string) error {
	if err := m.checkClosed(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.workers[name]
	if !exists {
		return NewWorkerNotFoundError(name)
	}

	// Stop the worker if running
	if w.IsRunning() {
		if err := w.Stop(); err != nil {
			return err
		}
	}

	delete(m.workers, name)

	// Remove from order slice
	for i, n := range m.order {
		if n == name {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}

	if m.logger != nil {
		m.logger.WithField("worker", name).Debug("worker unregistered")
	}

	return nil
}

// StartAll starts all registered workers.
// Workers are started in registration order.
func (m *WorkerManager) StartAll(ctx context.Context) error {
	if err := m.checkClosed(); err != nil {
		return err
	}

	if !m.running.CompareAndSwap(false, true) {
		return nil // Already running
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var startedWorkers []string
	for _, name := range m.order {
		w := m.workers[name]
		if err := w.Start(ctx); err != nil {
			// Rollback: stop already started workers
			for i := len(startedWorkers) - 1; i >= 0; i-- {
				if sw, ok := m.workers[startedWorkers[i]]; ok {
					_ = sw.Stop()
				}
			}
			m.running.Store(false)
			return err
		}
		startedWorkers = append(startedWorkers, name)

		if m.logger != nil {
			m.logger.WithField("worker", name).Info("worker started")
		}
	}

	if m.logger != nil {
		m.logger.WithField("count", len(startedWorkers)).Info("all workers started")
	}

	return nil
}

// StopAll stops all running workers.
// Workers are stopped in reverse registration order.
func (m *WorkerManager) StopAll() error {
	if !m.running.CompareAndSwap(true, false) {
		return nil // Already stopped
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var firstErr error
	// Stop in reverse order
	for i := len(m.order) - 1; i >= 0; i-- {
		name := m.order[i]
		w := m.workers[name]
		if w.IsRunning() {
			if err := w.Stop(); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				if m.logger != nil {
					m.logger.WithFields(map[string]interface{}{
						"worker": name,
						"error":  err.Error(),
					}).Warn("failed to stop worker")
				}
			} else if m.logger != nil {
				m.logger.WithField("worker", name).Info("worker stopped")
			}
		}
	}

	if m.logger != nil {
		m.logger.Info("all workers stopped")
	}

	return firstErr
}

// GetWorker returns a worker by name.
func (m *WorkerManager) GetWorker(name string) (Worker, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	w, exists := m.workers[name]
	if !exists {
		return nil, NewWorkerNotFoundError(name)
	}

	return w, nil
}

// ListWorkers returns the names of all registered workers.
func (m *WorkerManager) ListWorkers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, len(m.order))
	copy(names, m.order)
	return names
}

// GetMetrics returns metrics for all workers.
func (m *WorkerManager) GetMetrics() map[string]*WorkerMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := make(map[string]*WorkerMetrics, len(m.workers))
	for name, w := range m.workers {
		metrics[name] = w.Metrics()
	}

	return metrics
}

// GetAggregatedMetrics returns aggregated metrics across all workers.
func (m *WorkerManager) GetAggregatedMetrics() *WorkerMetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agg := &WorkerMetricsSnapshot{}
	for _, w := range m.workers {
		metrics := w.Metrics()
		agg.TasksProcessed += metrics.TasksProcessed.Load()
		agg.TasksFailed += metrics.TasksFailed.Load()
		agg.TasksRetried += metrics.TasksRetried.Load()
		agg.CurrentTasks += metrics.CurrentTasks.Load()
		agg.TotalProcessingTime += metrics.TotalProcessingTime.Load()

		lastTask := metrics.LastTaskTime.Load()
		if lastTask > agg.LastTaskTime {
			agg.LastTaskTime = lastTask
		}
	}

	return agg
}

// IsRunning returns true if the worker manager is running.
func (m *WorkerManager) IsRunning() bool {
	return m.running.Load()
}

// WorkerCount returns the number of registered workers.
func (m *WorkerManager) WorkerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.workers)
}

// RunningWorkerCount returns the number of currently running workers.
func (m *WorkerManager) RunningWorkerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, w := range m.workers {
		if w.IsRunning() {
			count++
		}
	}
	return count
}

// Close stops all workers and releases resources.
func (m *WorkerManager) Close() error {
	if m.closed.Swap(true) {
		return nil
	}

	err := m.StopAll()

	m.mu.Lock()
	m.workers = nil
	m.order = nil
	m.mu.Unlock()

	return err
}

// checkClosed returns an error if the manager is closed.
func (m *WorkerManager) checkClosed() error {
	if m.closed.Load() {
		return NewWorkerManagerClosedError()
	}
	return nil
}

// WorkerStatus represents the status of a worker.
type WorkerStatus struct {
	Name    string
	Running bool
	Metrics WorkerMetricsSnapshot
}

// GetStatus returns the status of all workers.
func (m *WorkerManager) GetStatus() []WorkerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]WorkerStatus, 0, len(m.workers))
	for name, w := range m.workers {
		status := WorkerStatus{
			Name:    name,
			Running: w.IsRunning(),
			Metrics: w.Metrics().Snapshot(),
		}
		statuses = append(statuses, status)
	}

	// Sort by name for consistent output
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})

	return statuses
}

// StartWorker starts a specific worker by name.
func (m *WorkerManager) StartWorker(ctx context.Context, name string) error {
	if err := m.checkClosed(); err != nil {
		return err
	}

	m.mu.RLock()
	w, exists := m.workers[name]
	m.mu.RUnlock()

	if !exists {
		return NewWorkerNotFoundError(name)
	}

	return w.Start(ctx)
}

// StopWorker stops a specific worker by name.
func (m *WorkerManager) StopWorker(name string) error {
	if err := m.checkClosed(); err != nil {
		return err
	}

	m.mu.RLock()
	w, exists := m.workers[name]
	m.mu.RUnlock()

	if !exists {
		return NewWorkerNotFoundError(name)
	}

	return w.Stop()
}

// RestartWorker restarts a specific worker by name.
func (m *WorkerManager) RestartWorker(ctx context.Context, name string) error {
	if err := m.StopWorker(name); err != nil {
		return err
	}
	return m.StartWorker(ctx, name)
}

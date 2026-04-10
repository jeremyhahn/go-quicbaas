//go:build rag

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

package ollama

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"
)

// HealthChecker provides health check functionality for the Ollama API.
type HealthChecker struct {
	client        *Client
	lastStatus    atomic.Value // stores *HealthStatus
	checkInterval time.Duration
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(client *Client) *HealthChecker {
	h := &HealthChecker{
		client:        client,
		checkInterval: client.healthCheckInterval,
	}

	// Initialize with unknown status
	h.lastStatus.Store(&HealthStatus{
		Available:   false,
		LastChecked: time.Time{},
	})

	return h
}

// Check performs a health check against the Ollama server.
func (h *HealthChecker) Check(ctx context.Context) (*HealthStatus, error) {
	status := &HealthStatus{
		LastChecked: time.Now(),
	}

	// Try to get version as a health check
	version, err := h.getVersion(ctx)
	if err != nil {
		status.Available = false
		status.Error = err.Error()
		h.lastStatus.Store(status)
		return status, err
	}

	status.Available = true
	status.Version = version
	h.lastStatus.Store(status)

	return status, nil
}

// IsAvailable returns the last known availability status without making a request.
func (h *HealthChecker) IsAvailable() bool {
	loaded := h.lastStatus.Load()
	if loaded == nil {
		return false
	}
	status, ok := loaded.(*HealthStatus)
	if !ok {
		return false
	}
	return status.Available
}

// LastStatus returns the last known health status without making a request.
func (h *HealthChecker) LastStatus() *HealthStatus {
	loaded := h.lastStatus.Load()
	if loaded == nil {
		return &HealthStatus{Available: false}
	}
	status, ok := loaded.(*HealthStatus)
	if !ok {
		return &HealthStatus{Available: false}
	}
	return status
}

// getVersion retrieves the Ollama version.
func (h *HealthChecker) getVersion(ctx context.Context) (string, error) {
	respBody, err := h.client.doRequest(ctx, "GET", "/api/version", nil)
	if err != nil {
		return "", err
	}

	var resp VersionResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", &OllamaSentinelCauseError{Sentinel: ErrInvalidResponse, Cause: err}
	}

	return resp.Version, nil
}

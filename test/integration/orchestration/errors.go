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

//go:build integration && rag

package orchestration

import "errors"

// Sentinel errors for orchestration integration test infrastructure.
var (
	// ErrTestSetupFailed indicates test environment setup failed.
	ErrTestSetupFailed = errors.New("orchestration integration: test setup failed")

	// ErrQuicBaaSUnavailable indicates the QuicBaaS service is not reachable.
	ErrQuicBaaSUnavailable = errors.New("orchestration integration: quicbaas unavailable")

	// ErrOllamaUnavailable indicates the Ollama service is not reachable.
	ErrOllamaUnavailable = errors.New("orchestration integration: ollama unavailable")

	// ErrTaskOperationFailed indicates a task store operation failed.
	ErrTaskOperationFailed = errors.New("orchestration integration: task operation failed")

	// ErrChannelOperationFailed indicates a channel server operation failed.
	ErrChannelOperationFailed = errors.New("orchestration integration: channel operation failed")

	// ErrAuthOperationFailed indicates an authentication/authorization operation failed.
	ErrAuthOperationFailed = errors.New("orchestration integration: auth operation failed")

	// ErrTenantOperationFailed indicates a tenant isolation operation failed.
	ErrTenantOperationFailed = errors.New("orchestration integration: tenant operation failed")

	// ErrHookOperationFailed indicates a hook receiver operation failed.
	ErrHookOperationFailed = errors.New("orchestration integration: hook operation failed")

	// ErrInvalidResponse indicates an unexpected response from the service.
	ErrInvalidResponse = errors.New("orchestration integration: invalid response")

	// ErrHealthCheckFailed indicates a health check operation failed.
	ErrHealthCheckFailed = errors.New("orchestration integration: health check failed")
)

// QuicBaaSUnavailableError provides details about a QuicBaaS connectivity failure.
type QuicBaaSUnavailableError struct {
	URL   string
	Cause error
}

// Error implements the error interface.
func (e *QuicBaaSUnavailableError) Error() string {
	if e.Cause != nil {
		return ErrQuicBaaSUnavailable.Error() + ": " + e.URL + ": " + e.Cause.Error()
	}
	return ErrQuicBaaSUnavailable.Error() + ": " + e.URL
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *QuicBaaSUnavailableError) Unwrap() error {
	return ErrQuicBaaSUnavailable
}

// OllamaUnavailableError provides details about an Ollama connectivity failure.
type OllamaUnavailableError struct {
	URL   string
	Cause error
}

// Error implements the error interface.
func (e *OllamaUnavailableError) Error() string {
	if e.Cause != nil {
		return ErrOllamaUnavailable.Error() + ": " + e.URL + ": " + e.Cause.Error()
	}
	return ErrOllamaUnavailable.Error() + ": " + e.URL
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *OllamaUnavailableError) Unwrap() error {
	return ErrOllamaUnavailable
}

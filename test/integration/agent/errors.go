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

package agent

import "errors"

// Sentinel errors for typed error handling per project conventions.
var (
	// ErrTestSetupFailed indicates test environment setup failed.
	ErrTestSetupFailed = errors.New("agent integration: test setup failed")

	// ErrOllamaUnavailable indicates the Ollama service is not reachable.
	ErrOllamaUnavailable = errors.New("agent integration: ollama unavailable")

	// ErrPromptFailed indicates an agent prompt operation failed.
	ErrPromptFailed = errors.New("agent integration: prompt failed")

	// ErrDiscoveryFailed indicates project discovery failed.
	ErrDiscoveryFailed = errors.New("agent integration: discovery failed")

	// ErrRegistrationFailed indicates project registration failed.
	ErrRegistrationFailed = errors.New("agent integration: registration failed")

	// ErrDecompositionFailed indicates task decomposition failed.
	ErrDecompositionFailed = errors.New("agent integration: decomposition failed")

	// ErrCacheFailed indicates an agent cache operation failed.
	ErrCacheFailed = errors.New("agent integration: cache operation failed")

	// ErrInvalidResponse indicates an unexpected response from the backend.
	ErrInvalidResponse = errors.New("agent integration: invalid response")
)

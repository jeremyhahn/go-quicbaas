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

package watch

// All watch manager tests use time.Sleep for goroutine synchronization and
// real goroutine spawning, making them integration tests rather than pure unit tests.
//
// These tests have been moved to manager_integration_test.go with proper build tags.
//
// To run these tests:
//   go test -tags=integration ./pkg/watch/...

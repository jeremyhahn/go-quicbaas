// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package grpc

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// newTestLogger builds a debug-level, JSON-formatted test logger shared by
// this package's test files. It used to live in server_xkms_test.go
// (retired with the rest of the xkms.v1.KeystoreService duplicate,
// Phase 3 of the login-plane plan — see server_xkms.go's deletion), which
// several unrelated test files (server_ssh_test.go,
// server_kms_rotation_policy_test.go, server_kms_admin_test.go) still
// depend on; it now lives in this package-wide test helper file instead.
func newTestLogger() *logger.Logger {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	return log
}

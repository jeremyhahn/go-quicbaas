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

//go:build integration

package cluster

import (
	"net/http"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// e2eTenantClient returns a client that calls as the tenant operator. It
// builds under the integration tag alone because the failover, membership
// and scheduler-proof tests share it with the rest-tagged E2E tests.
func e2eTenantClient(t *testing.T) *http.Client {
	t.Helper()
	return helpers.NewTenantOperatorHTTPClient(t, 30*time.Second)
}

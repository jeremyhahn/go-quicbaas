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

package grants

import "github.com/jeremyhahn/go-quicbaas/pkg/iam"

// The go-qrdb IAM binder for SPIFFE workload principals is the RBAC grant's
// RoleBinder.
var _ RoleBinder = (*iam.WorkloadRoleBinder)(nil)

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

package quicbaas

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Admin-factor re-exports. Every daemon in the stack parses the
// `auth.admin_factors` setting through qrdb's one parser, so a downstream
// consumer (e.g. go-trusted-platform) validates the same setting at config
// load through this SDK rather than keeping its own copy of the vocabulary.
// Pure aliases — no transport state, no behavior.

// AdminFactor names one factor enrolled for the first-boot administrator.
type AdminFactor = qrdbsdk.AdminFactor

// AdminEnrollmentPlan is the ordered set of factors to enroll for the
// administrator, as produced by ParseAdminFactors.
type AdminEnrollmentPlan = qrdbsdk.AdminEnrollmentPlan

// UnsupportedAdminFactorError is returned by ParseAdminFactors for a factor
// name this build cannot enroll. Match it with errors.As.
type UnsupportedAdminFactorError = qrdbsdk.UnsupportedAdminFactorError

// ParseAdminFactors turns operator-supplied admin-factor names into an
// AdminEnrollmentPlan. It accepts repeated values and comma-separated lists
// interchangeably; an empty list yields the mTLS baseline.
var ParseAdminFactors = qrdbsdk.ParseAdminFactors

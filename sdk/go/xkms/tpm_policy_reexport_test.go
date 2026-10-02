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

package xkms_test

import (
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms"
)

// Compile-time proof the TPM-policy wire types that replaced go-xkms's removed
// Policy* service (go-xkms tpm-policies, v0.45) are the go-xkms types, so a
// consumer's xkms client implementation and its test doubles keep matching
// the upstream client interface.
var (
	_ xkmssdk.TPMPolicy                  = xkms.TPMPolicy{}
	_ xkms.TPMPolicy                     = xkmssdk.TPMPolicy{}
	_ xkmssdk.TPMPolicyBranch            = xkms.TPMPolicyBranch{}
	_ xkmssdk.TPMPolicyCondition         = xkms.TPMPolicyCondition{}
	_ xkmssdk.TPMPolicyCreateRequest     = xkms.TPMPolicyCreateRequest{}
	_ xkmssdk.TPMPolicyRequest           = xkms.TPMPolicyRequest{}
	_ xkmssdk.TPMPolicyListResponse      = xkms.TPMPolicyListResponse{}
	_ xkmssdk.TPMPolicyVerifyResponse    = xkms.TPMPolicyVerifyResponse{}
	_ xkmssdk.TPMObjectPolicyRequest     = xkms.TPMObjectPolicyRequest{}
	_ xkmssdk.TPMAuthorityRotateResponse = xkms.TPMAuthorityRotateResponse{}
	_ xkmssdk.TenantTPMModeResponse      = xkms.TenantTPMModeResponse{}
	_ xkmssdk.TenantVTPMStatusResponse   = xkms.TenantVTPMStatusResponse{}
)

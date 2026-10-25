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

package service

import (
	stderrors "errors"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestRefuseTenantLogin_SystemTenantPasses checks that the system tenant's
// login is never refused.
func TestRefuseTenantLogin_SystemTenantPasses(t *testing.T) {
	if err := RefuseTenantLogin(qrdbsdk.SystemTenantID); err != nil {
		t.Fatalf("RefuseTenantLogin(system) = %v, want nil", err)
	}
}

// TestRefuseTenantLogin_OtherTenantsRefused checks that every other tenant,
// including an empty one, gets the typed refusal as a PreconditionFailed
// QBError whose message names go-quicbaas #1816.
func TestRefuseTenantLogin_OtherTenantsRefused(t *testing.T) {
	for _, tenantID := range []string{"acme", ""} {
		err := RefuseTenantLogin(tenantID)
		var refused *sdkerrors.TenantLoginRefusedError
		if !stderrors.As(err, &refused) {
			t.Fatalf("RefuseTenantLogin(%q) = %v, want TenantLoginRefusedError", tenantID, err)
		}
		var coded *dberrors.QBError
		if !stderrors.As(err, &coded) || coded.Code != dberrors.ErrPreconditionFailed {
			t.Errorf("RefuseTenantLogin(%q) = %v, want a PreconditionFailed QBError", tenantID, err)
		}
		if got := refused.Error(); !strings.Contains(got, "v0.54.0-alpha") || !strings.Contains(got, "#1816") {
			t.Errorf("refusal message %q must name go-qrdb v0.54.0-alpha and #1816", got)
		}
	}
}

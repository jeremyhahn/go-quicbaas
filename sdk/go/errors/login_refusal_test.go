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

package errors

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestNewTenantLoginRefused verifies the refusal is a failed precondition that
// still exposes the typed refusal and names the release that lifts it.
func TestNewTenantLoginRefused(t *testing.T) {
	err := NewTenantLoginRefused("auth.FinishLogin")

	if got := Code(err); got != CodePreconditionFailed {
		t.Errorf("Code = %s, want %s", got, CodePreconditionFailed)
	}
	if err.Op != "auth.FinishLogin" {
		t.Errorf("Op = %q, want %q", err.Op, "auth.FinishLogin")
	}
	var refused *TenantLoginRefusedError
	if !errors.As(err, &refused) {
		t.Fatal("errors.As(TenantLoginRefusedError) = false, want true")
	}
	for _, want := range []string{"v0.54.0-alpha", "#1084", "#1089", "#1816"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Error() = %q, want it to name %s", err.Error(), want)
		}
	}
}

// TestTenantLoginRefused_WrappedAndUnrelated verifies the typed refusal is
// found through further wrapping and is not found in an unrelated error.
func TestTenantLoginRefused_WrappedAndUnrelated(t *testing.T) {
	wrapped := fmt.Errorf("transport: %w", NewTenantLoginRefused("auth.FinishLogin"))
	var refused *TenantLoginRefusedError
	if !errors.As(wrapped, &refused) {
		t.Error("errors.As on a wrapped refusal = false, want true")
	}

	other := New(CodePreconditionFailed, "auth.FinishLogin", "barrier sealed")
	if errors.As(other, &refused) {
		t.Error("errors.As on an unrelated precondition failure = true, want false")
	}
}

// TestRestoreTenantLoginRefused: a remote failed precondition carrying the
// refusal's guidance becomes the typed refusal under the caller's op.
func TestRestoreTenantLoginRefused(t *testing.T) {
	remote := New(CodePreconditionFailed, "iamRESTHandler.doRequest",
		"tenant_login_refused: "+(&TenantLoginRefusedError{}).Error())

	err := RestoreTenantLoginRefused("quicbaas.IAM.Invite.ActivateFIDO2Begin", remote)

	var refused *TenantLoginRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want TenantLoginRefusedError", err)
	}
	var qbErr *QBError
	if !errors.As(err, &qbErr) || qbErr.Op != "quicbaas.IAM.Invite.ActivateFIDO2Begin" || qbErr.Code != CodePreconditionFailed {
		t.Errorf("err = %+v, want the refusal under the caller's op", err)
	}
}

// TestRestoreTenantLoginRefused_OtherErrorsUnchanged: a failed precondition
// without the guidance, another code, and an already typed refusal pass
// through as they are.
func TestRestoreTenantLoginRefused_OtherErrorsUnchanged(t *testing.T) {
	notPending := New(CodePreconditionFailed, "iamHandler.handleInviteRequest", "user is not in pending state")
	internal := New(CodeInternal, "iamRESTHandler.doRequest", (&TenantLoginRefusedError{}).Error())
	typed := NewTenantLoginRefused("auth.FinishLogin")

	for name, in := range map[string]error{"not pending": notPending, "internal": internal, "typed": typed} {
		if got := RestoreTenantLoginRefused("op", in); got != in {
			t.Errorf("%s: RestoreTenantLoginRefused = %v, want the input unchanged", name, got)
		}
	}
	var refused *TenantLoginRefusedError
	if errors.As(RestoreTenantLoginRefused("op", notPending), &refused) {
		t.Error("a precondition failure without the guidance became the refusal")
	}
}

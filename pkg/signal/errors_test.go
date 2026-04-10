//go:build quicbaas_signal

package signal

import (
	"errors"
	"strings"
	"testing"
)

func TestSignalNotEnabledError_Message(t *testing.T) {
	err := &SignalNotEnabledError{TenantID: "tenant-abc"}
	got := err.Error()
	if got != "signal: not enabled for tenant tenant-abc" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestSignalNotEnabledError_EmptyTenant(t *testing.T) {
	err := &SignalNotEnabledError{TenantID: ""}
	got := err.Error()
	if !strings.Contains(got, "signal: not enabled for tenant") {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestSignalNotEnabledError_AsInterface(t *testing.T) {
	var target *SignalNotEnabledError
	err := error(&SignalNotEnabledError{TenantID: "t1"})
	if !errors.As(err, &target) {
		t.Fatal("expected errors.As to match SignalNotEnabledError")
	}
	if target.TenantID != "t1" {
		t.Fatalf("expected TenantID t1, got %s", target.TenantID)
	}
}

func TestTenantNotFoundError_Message(t *testing.T) {
	err := &TenantNotFoundError{TenantID: "missing-tenant"}
	got := err.Error()
	if got != "signal: tenant not found: missing-tenant" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestTenantNotFoundError_EmptyTenant(t *testing.T) {
	err := &TenantNotFoundError{TenantID: ""}
	got := err.Error()
	if got != "signal: tenant not found: " {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestTenantNotFoundError_AsInterface(t *testing.T) {
	var target *TenantNotFoundError
	err := error(&TenantNotFoundError{TenantID: "xyz"})
	if !errors.As(err, &target) {
		t.Fatal("expected errors.As to match TenantNotFoundError")
	}
	if target.TenantID != "xyz" {
		t.Fatalf("expected TenantID xyz, got %s", target.TenantID)
	}
}

func TestTenantAlreadyProvisionedError_Message(t *testing.T) {
	err := &TenantAlreadyProvisionedError{TenantID: "dup-tenant"}
	got := err.Error()
	if got != "signal: tenant already provisioned: dup-tenant" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestTenantAlreadyProvisionedError_EmptyTenant(t *testing.T) {
	err := &TenantAlreadyProvisionedError{TenantID: ""}
	got := err.Error()
	if got != "signal: tenant already provisioned: " {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestTenantAlreadyProvisionedError_AsInterface(t *testing.T) {
	var target *TenantAlreadyProvisionedError
	err := error(&TenantAlreadyProvisionedError{TenantID: "dup"})
	if !errors.As(err, &target) {
		t.Fatal("expected errors.As to match TenantAlreadyProvisionedError")
	}
}

func TestAccountQuotaExceededError_Message(t *testing.T) {
	err := &AccountQuotaExceededError{TenantID: "quota-tenant", MaxAccounts: 100}
	got := err.Error()
	if got != "signal: account quota exceeded for tenant quota-tenant" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestAccountQuotaExceededError_Fields(t *testing.T) {
	err := &AccountQuotaExceededError{TenantID: "t2", MaxAccounts: 50}
	if err.TenantID != "t2" {
		t.Fatalf("expected TenantID t2, got %s", err.TenantID)
	}
	if err.MaxAccounts != 50 {
		t.Fatalf("expected MaxAccounts 50, got %d", err.MaxAccounts)
	}
}

func TestAccountQuotaExceededError_AsInterface(t *testing.T) {
	var target *AccountQuotaExceededError
	err := error(&AccountQuotaExceededError{TenantID: "t", MaxAccounts: 10})
	if !errors.As(err, &target) {
		t.Fatal("expected errors.As to match AccountQuotaExceededError")
	}
}

func TestInvalidConfigError_Message(t *testing.T) {
	err := &InvalidConfigError{Field: "maxAccounts", Reason: "must not be negative"}
	got := err.Error()
	expected := "signal: invalid config: maxAccounts: must not be negative"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestInvalidConfigError_EmptyFieldAndReason(t *testing.T) {
	err := &InvalidConfigError{Field: "", Reason: ""}
	got := err.Error()
	if got != "signal: invalid config: : " {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestInvalidConfigError_AsInterface(t *testing.T) {
	var target *InvalidConfigError
	err := error(&InvalidConfigError{Field: "f", Reason: "r"})
	if !errors.As(err, &target) {
		t.Fatal("expected errors.As to match InvalidConfigError")
	}
	if target.Field != "f" || target.Reason != "r" {
		t.Fatalf("unexpected field/reason: %s/%s", target.Field, target.Reason)
	}
}

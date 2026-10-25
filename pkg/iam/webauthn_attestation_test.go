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

package iam

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// yubiKey5AAGUID is a plausible hardware authenticator model identifier used as
// the allowlisted model throughout these tests. Its value is arbitrary — what
// matters is that the gate distinguishes allowlisted from not.
var yubiKey5AAGUID = []byte{
	0xcb, 0x69, 0x48, 0x1e, 0x8f, 0xf7, 0x40, 0x39,
	0x93, 0xec, 0x0a, 0x27, 0x29, 0xa1, 0x54, 0xa8,
}

// virtualAAGUID stands in for a CDP virtual authenticator, which reports an
// all-zero AAGUID.
var virtualAAGUID = make([]byte, 16)

// stubAdminResolver answers the gate's admin question from a fixed table.
type stubAdminResolver struct {
	admins map[string]bool
	err    error
}

func (s *stubAdminResolver) IsAdministrativeAccount(_ context.Context, _, email string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.admins[email], nil
}

// permissiveTestGate returns a gate that admits any authenticator for any
// account. It exists so the write-through tests that are ABOUT cascade and
// compensation behaviour are not also asserting attestation policy; the
// attestation policy is asserted in this file.
func permissiveTestGate() *WebAuthnAttestationGate {
	return NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		NewAAGUIDAllowlist(nil, AAGUIDPolicyPermissive),
		&stubAdminResolver{admins: map[string]bool{}},
	)
}

// verifiedChainContext returns a context carrying a verified attestation trust
// path for the given AAGUID — what a ceremony entry point stamps after checking
// the x5c chain against the operator's attestation roots.
//
// Building one requires being inside pkg/iam, which is the point: a caller
// outside this package cannot fabricate the evidence the gate demands.
func verifiedChainContext(aaguid []byte) context.Context {
	return contextWithAttestationChain(context.Background(), &WebAuthnAttestationChain{
		verified: true,
		aaguid:   hex.EncodeToString(aaguid),
	})
}

// hardwareAllowlist returns a strict allowlist holding only the hardware model.
func hardwareAllowlist() *AAGUIDAllowlist {
	return NewAAGUIDAllowlist([]string{hex.EncodeToString(yubiKey5AAGUID)}, AAGUIDPolicyStrict)
}

// -----------------------------------------------------------------------------
// The zero value is the strict one
// -----------------------------------------------------------------------------

// TestWebAuthnAttestationGate_ZeroValueRefusesEverything is the "safe by doing
// nothing" regression. A gate whose fields nobody set must refuse, because the
// old code's equivalent state — no allowlist wired — accepted every
// authenticator on every path.
func TestWebAuthnAttestationGate_ZeroValueRefusesEverything(t *testing.T) {
	var gate WebAuthnAttestationGate

	for _, tc := range []struct {
		name            string
		attestationType string
		aaguid          []byte
	}{
		{"no attestation", "none", nil},
		{"self attestation", "basic_surrogate", virtualAAGUID},
		{"hardware attestation, unknown model", "basic_full", yubiKey5AAGUID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := gate.Evaluate(context.Background(), "t1", "op@example.com", tc.attestationType, tc.aaguid)
			if err == nil {
				t.Fatalf("zero-value gate accepted %s; want refusal", tc.name)
			}
			if result != nil {
				t.Fatalf("a refusal must return no result, got %+v", result)
			}
		})
	}
}

// TestWebAuthnAttestationGate_NilGateRefuses covers the nil-pointer path a
// caller reaches by forgetting to construct the gate at all.
func TestWebAuthnAttestationGate_NilGateRefuses(t *testing.T) {
	var gate *WebAuthnAttestationGate

	result, err := gate.Evaluate(context.Background(), "t1", "op@example.com", "basic_full", yubiKey5AAGUID)
	if err == nil {
		t.Fatal("a nil gate accepted a registration; want refusal")
	}
	if result != nil {
		t.Fatalf("a refusal must return no result, got %+v", result)
	}
	if gate.Assurance() != WebAuthnAssuranceHardware {
		t.Fatalf("nil gate Assurance = %v, want hardware", gate.Assurance())
	}
}

// -----------------------------------------------------------------------------
// A software authenticator is refused for an administrator
// -----------------------------------------------------------------------------

// TestWebAuthnAttestationGate_RefusesVirtualAuthenticatorForAdmin is issue
// #381's headline verification: a virtual/software authenticator must be
// refused for administrative enrollment with an operator-facing reason.
func TestWebAuthnAttestationGate_RefusesVirtualAuthenticatorForAdmin(t *testing.T) {
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny, // tenant users may be permissive...
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{"admin@example.com": true}},
	)

	// ...but the administrator is not.
	result, err := gate.Evaluate(context.Background(), "t1", "admin@example.com", "none", virtualAAGUID)
	if err == nil {
		t.Fatal("a virtual authenticator was accepted as an administrator credential")
	}
	if result != nil {
		t.Fatalf("a refusal must return no result, got %+v", result)
	}

	var required *AttestationRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("want *AttestationRequiredError, got %T: %v", err, err)
	}
	if required.Proven != AttestLevelNone {
		t.Errorf("Proven = %q, want %q", required.Proven, AttestLevelNone)
	}
	if required.Required != AttestLevelHardware {
		t.Errorf("Required = %q, want %q", required.Required, AttestLevelHardware)
	}
	// The refusal has to tell an operator what to do about it.
	if msg := required.Error(); !strings.Contains(msg, "hardware security key") {
		t.Errorf("refusal gives no operator guidance: %q", msg)
	}
}

func TestWebAuthnAttestationGate_AssuranceAnyRefusesAdminRoleID(t *testing.T) {
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		NewWebAuthnAdminResolver(&stubAttestUserResolver{users: map[string]*User{
			"admin@example.com": {Email: "admin@example.com", Roles: []string{"role-admin-id"}},
		}}, stubAttestRoles(), nil),
	)

	result, err := gate.Evaluate(context.Background(), "t1", "admin@example.com", "none", virtualAAGUID)
	if err == nil {
		t.Fatal("assurance any accepted a software administrator credential")
	}
	if result != nil {
		t.Fatalf("a refusal must return no result, got %+v", result)
	}

	var required *AttestationRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("want *AttestationRequiredError, got %T: %v", err, err)
	}
	if required.Required != AttestLevelHardware {
		t.Errorf("Required = %q, want %q", required.Required, AttestLevelHardware)
	}
}

// TestWebAuthnAttestationGate_RefusesSelfAttestationForAdmin covers the second
// software case: a well-formed but self-signed attestation statement, which is
// syntactically an attestation and proves nothing about key storage.
func TestWebAuthnAttestationGate_RefusesSelfAttestationForAdmin(t *testing.T) {
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)

	_, err := gate.Evaluate(context.Background(), "t1", "admin@example.com", "basic_surrogate", yubiKey5AAGUID)
	if err == nil {
		t.Fatal("self attestation was accepted where hardware was required")
	}
	var required *AttestationRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("want *AttestationRequiredError, got %T: %v", err, err)
	}
	if required.Proven != AttestLevelSoftware {
		t.Errorf("Proven = %q, want %q", required.Proven, AttestLevelSoftware)
	}
	if msg := required.Error(); !strings.Contains(msg, "self-signed") {
		t.Errorf("refusal does not name self-signing as the problem: %q", msg)
	}
}

// TestWebAuthnAttestationGate_AcceptsAllowlistedHardware is the positive case:
// a manufacturer-attested authenticator of an allowlisted model is admitted and
// reports hardware.
func TestWebAuthnAttestationGate_AcceptsAllowlistedHardware(t *testing.T) {
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)

	for _, attestationType := range []string{"basic_full", "attca", "anonca"} {
		t.Run(attestationType, func(t *testing.T) {
			result, err := gate.Evaluate(verifiedChainContext(yubiKey5AAGUID), "t1", "admin@example.com", attestationType, yubiKey5AAGUID)
			if err != nil {
				t.Fatalf("allowlisted hardware refused: %v", err)
			}
			if !result.IsHardware() {
				t.Fatalf("Level = %q, want hardware", result.Level())
			}
			if got, want := result.AAGUID(), hex.EncodeToString(yubiKey5AAGUID); got != want {
				t.Errorf("AAGUID = %q, want %q", got, want)
			}
			if result.Method() != AttestMethodWebAuthn {
				t.Errorf("Method = %q, want %q", result.Method(), AttestMethodWebAuthn)
			}
		})
	}
}

// TestWebAuthnAttestationGate_RefusesHardwareOfUnknownModel proves the AAGUID
// allowlist still bites once attestation makes the AAGUID meaningful: a real
// security key of a model the operator did not allowlist is refused.
func TestWebAuthnAttestationGate_RefusesHardwareOfUnknownModel(t *testing.T) {
	other := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)

	if _, err := gate.Evaluate(verifiedChainContext(other), "t1", "admin@example.com", "basic_full", other); err == nil {
		t.Fatal("an authenticator of a non-allowlisted model was accepted")
	}
}

// TestWebAuthnAttestationGate_NilAllowlistDeniesAttestedHardware is the
// regression #381 asks for by name: a nil allowlist must DENY rather than
// allow, even for a genuinely manufacturer-attested authenticator. The old
// `if allowlist != nil` guard made this exact case a silent pass.
func TestWebAuthnAttestationGate_NilAllowlistDeniesAttestedHardware(t *testing.T) {
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, nil, nil)

	result, err := gate.Evaluate(verifiedChainContext(yubiKey5AAGUID), "t1", "admin@example.com", "basic_full", yubiKey5AAGUID)
	if err == nil {
		t.Fatal("a nil AAGUID allowlist accepted an enrollment; an unconfigured gate must deny")
	}
	if result != nil {
		t.Fatalf("a refusal must return no result, got %+v", result)
	}
	var unconfigured *ErrAAGUIDGateUnconfiguredError
	if !errors.As(err, &unconfigured) {
		t.Fatalf("want *ErrAAGUIDGateUnconfiguredError, got %T: %v", err, err)
	}
	if msg := err.Error(); !strings.Contains(msg, "iam.aaguid.allowlist_file") {
		t.Errorf("refusal does not name the missing setting: %q", msg)
	}
}

// -----------------------------------------------------------------------------
// Tenant-permissive policy, and what it may not reach
// -----------------------------------------------------------------------------

// TestWebAuthnAttestationGate_PermissiveTenantRecordsSoftware shows the
// explicit opt-in working: a non-administrative account may enroll an
// unattested authenticator, and the result records that it proved nothing —
// so the credential is visibly software-backed rather than indistinguishable
// from hardware.
func TestWebAuthnAttestationGate_PermissiveTenantRecordsSoftware(t *testing.T) {
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		nil, // no allowlist at all: still fine, because hardware is not required here
		&stubAdminResolver{admins: map[string]bool{"admin@example.com": true}},
	)

	result, err := gate.Evaluate(context.Background(), "t1", "user@example.com", "none", virtualAAGUID)
	if err != nil {
		t.Fatalf("permissive tenant enrollment refused: %v", err)
	}
	if result.Level() != AttestLevelNone {
		t.Errorf("Level = %q, want %q", result.Level(), AttestLevelNone)
	}
	if result.IsHardware() {
		t.Fatal("an unattested credential reported hardware")
	}
}

// TestWebAuthnAttestationGate_UnresolvableAccountIsTreatedAsAdmin is the
// self-service-registration case. That flow completes the WebAuthn ceremony
// BEFORE creating the user and then grants that user Admin, so the account
// cannot be resolved at gate time. Reading "cannot resolve" as "not an
// administrator" would let a tenant-permissive deployment enroll a virtual
// authenticator as a tenant administrator.
func TestWebAuthnAttestationGate_UnresolvableAccountIsTreatedAsAdmin(t *testing.T) {
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{err: errors.New("user not found")},
	)

	if _, err := gate.Evaluate(context.Background(), "t1", "brand-new@example.com", "none", virtualAAGUID); err == nil {
		t.Fatal("an unresolvable account was admitted under the permissive tenant policy")
	}
}

// TestWebAuthnAttestationGate_AbsentResolverIsTreatedAsAdmin covers the same
// fail-safe direction when no resolver is wired at all.
func TestWebAuthnAttestationGate_AbsentResolverIsTreatedAsAdmin(t *testing.T) {
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceAny, hardwareAllowlist(), nil)

	if _, err := gate.Evaluate(context.Background(), "t1", "user@example.com", "none", virtualAAGUID); err == nil {
		t.Fatal("a gate with no admin resolver admitted an unattested authenticator")
	}
}

// TestWebAuthnAttestationGate_UnidentifiedRegistrantIsTreatedAsAdmin pins the
// decision to the gate rather than to the resolver's manners. A resolver is
// free to answer "no such account, therefore not an administrator" for an empty
// email; the gate must not accept that answer, because an unidentified
// registrant is exactly the case it cannot afford to get wrong.
func TestWebAuthnAttestationGate_UnidentifiedRegistrantIsTreatedAsAdmin(t *testing.T) {
	// A deliberately lenient resolver: every unknown email answers
	// (false, nil) — "not an administrator" — with no error to fall back on.
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{}},
	)

	for _, email := range []string{"", "   "} {
		if _, err := gate.Evaluate(context.Background(), "t1", email, "none", virtualAAGUID); err == nil {
			t.Fatalf("an unidentified registrant (email %q) was admitted under the permissive tenant policy", email)
		}
	}

	// A registrant that IS identified and is not an administrator still gets
	// the permissive policy, so the guard above has not simply disabled it.
	if _, err := gate.Evaluate(context.Background(), "t1", "user@example.com", "none", virtualAAGUID); err != nil {
		t.Fatalf("an identified tenant user was refused under the permissive policy: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Assurance parsing
// -----------------------------------------------------------------------------

// TestParseWebAuthnAssurance covers the mapping and, critically, that an empty
// value selects hardware rather than the loose policy.
func TestParseWebAuthnAssurance(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want WebAuthnAssurance
	}{
		{"", WebAuthnAssuranceHardware},
		{"hardware", WebAuthnAssuranceHardware},
		{"HARDWARE", WebAuthnAssuranceHardware},
		{"  any  ", WebAuthnAssuranceAny},
		{"insecure-allow-software-administrators", WebAuthnAssuranceInsecureAllowSoftwareAdministrators},
		{"INSECURE-ALLOW-SOFTWARE-ADMINISTRATORS", WebAuthnAssuranceInsecureAllowSoftwareAdministrators},
	} {
		got, err := ParseWebAuthnAssurance(tc.in)
		if err != nil {
			t.Fatalf("ParseWebAuthnAssurance(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ParseWebAuthnAssurance(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestParseWebAuthnAssurance_RejectsUnknown proves a typo is refused rather
// than silently selecting a policy — and that the value it falls back to on the
// error path is the strict one.
func TestParseWebAuthnAssurance_RejectsUnknown(t *testing.T) {
	got, err := ParseWebAuthnAssurance("hardwar")
	if err == nil {
		t.Fatal("an unrecognised assurance value was accepted")
	}
	if got != WebAuthnAssuranceHardware {
		t.Errorf("on error the value must be the strict one, got %v", got)
	}
	var valueErr *WebAuthnAssuranceValueError
	if !errors.As(err, &valueErr) {
		t.Fatalf("want *WebAuthnAssuranceValueError, got %T", err)
	}
}

// -----------------------------------------------------------------------------
// Attestation type mapping
// -----------------------------------------------------------------------------

// TestWebAuthnAttestLevel checks the FIDO taxonomy mapping, including that an
// unrecognised type degrades to "none" rather than being optimistically read as
// evidence.
func TestWebAuthnAttestLevel(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want AttestLevel
	}{
		{"basic_full", AttestLevelHardware},
		{"attca", AttestLevelHardware},
		{"anonca", AttestLevelHardware},
		{"BASIC_FULL", AttestLevelHardware},
		{"basic_surrogate", AttestLevelSoftware},
		{"self", AttestLevelSoftware},
		{"none", AttestLevelNone},
		{"", AttestLevelNone},
		{"something-new-from-the-future", AttestLevelNone},
	} {
		if got := webAuthnAttestLevel(tc.in); got != tc.want {
			t.Errorf("webAuthnAttestLevel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// -----------------------------------------------------------------------------
// Admin resolution over the canonical store
// -----------------------------------------------------------------------------

// stubAttestUserResolver answers GetUserByEmail from a fixed table, recording
// the tenant the resolver scoped the lookup to.
type stubAttestUserResolver struct {
	users map[string]*User
	err   error
	// gotTenantID is the tenant the last lookup was scoped to.
	gotTenantID string
}

type stubAttestRoleResolver struct {
	roles map[string]*qrdbsdk.Role
	err   error
}

func (s *stubAttestRoleResolver) GetRoleByName(_ context.Context, tenantID, name string) (*qrdbsdk.Role, error) {
	if s.err != nil {
		return nil, s.err
	}
	for _, role := range s.roles {
		if role.TenantID == tenantID && role.Name == name {
			return role, nil
		}
	}
	return nil, errors.New("role not found")
}

func stubAttestRoles() *stubAttestRoleResolver {
	return &stubAttestRoleResolver{roles: map[string]*qrdbsdk.Role{
		"role-admin-id": {ID: "role-admin-id", TenantID: "t1", Name: qrdbsdk.RoleNameAdmin},
		"role-so-id":    {ID: "role-so-id", TenantID: "t1", Name: qrdbsdk.RoleNameSO},
		"role-user-id":  {ID: "role-user-id", TenantID: "t1", Name: "User"},
		"role-audit-id": {ID: "role-audit-id", TenantID: "t1", Name: "Auditor"},
	}}
}

// GetUserByEmail records the tenant it was asked for, so a test can assert the
// resolver passes the tenant EXPLICITLY rather than relying on the context —
// the lookup is tenant-scoped (#359) and a stub that ignored the argument would
// pass whether or not the caller supplied it.
func (s *stubAttestUserResolver) GetUserByEmail(_ context.Context, tenantID, email string) (*User, error) {
	s.gotTenantID = tenantID
	if s.err != nil {
		return nil, s.err
	}
	u, ok := s.users[email]
	if !ok {
		return nil, errors.New("user not found")
	}
	return u, nil
}

// TestAdminRoleResolver recognises the administrative roles and reports a plain
// tenant user as non-administrative.
func TestAdminRoleResolver(t *testing.T) {
	resolver := NewWebAuthnAdminResolver(&stubAttestUserResolver{users: map[string]*User{
		"admin@example.com":  {Email: "admin@example.com", Roles: []string{"role-user-id", "role-admin-id"}},
		"so@example.com":     {Email: "so@example.com", Roles: []string{"role-so-id"}},
		"tenant@example.com": {Email: "tenant@example.com", Roles: []string{"role-user-id", "role-audit-id"}},
	}}, stubAttestRoles(), nil)

	for email, want := range map[string]bool{
		"admin@example.com":  true,
		"so@example.com":     true,
		"tenant@example.com": false,
	} {
		got, err := resolver.IsAdministrativeAccount(context.Background(), "t1", email)
		if err != nil {
			t.Fatalf("IsAdministrativeAccount(%s): %v", email, err)
		}
		if got != want {
			t.Errorf("IsAdministrativeAccount(%s) = %v, want %v", email, got, want)
		}
	}

	failingRoles := NewWebAuthnAdminResolver(&stubAttestUserResolver{users: map[string]*User{
		"tenant@example.com": {Email: "tenant@example.com", Roles: []string{"role-user-id"}},
	}}, &stubAttestRoleResolver{err: errors.New("role resolver unavailable")}, nil)
	got, err := failingRoles.IsAdministrativeAccount(context.Background(), "t1", "tenant@example.com")
	var roleErr *WebAuthnRoleResolverError
	if !errors.As(err, &roleErr) {
		t.Fatalf("IsAdministrativeAccount with failing role resolver: err = %v, want *WebAuthnRoleResolverError", err)
	}
	if got {
		t.Fatal("role resolver failure was reported as an answer of administrative")
	}

	failingBind := NewWebAuthnAdminResolver(&stubAttestUserResolver{users: map[string]*User{
		"tenant@example.com": {Email: "tenant@example.com", Roles: []string{"role-user-id"}},
	}}, stubAttestRoles(), func(context.Context, string) (context.Context, error) {
		return nil, errors.New("service principal unavailable")
	})
	if _, err := failingBind.IsAdministrativeAccount(context.Background(), "t1", "tenant@example.com"); !errors.As(err, &roleErr) {
		t.Fatalf("IsAdministrativeAccount with failing role-read binder: err = %v, want *WebAuthnRoleResolverError", err)
	}
}

// TestAdminRoleResolver_ErrorsAreNotFalse is the fail-safe contract: an
// unresolvable account must return an error, NOT (false, nil). A false would
// be read by the gate as "tenant user" and could downgrade an administrator.
func TestAdminRoleResolver_ErrorsAreNotFalse(t *testing.T) {
	resolver := NewWebAuthnAdminResolver(&stubAttestUserResolver{err: errors.New("store unavailable")}, stubAttestRoles(), nil)

	if _, err := resolver.IsAdministrativeAccount(context.Background(), "t1", "admin@example.com"); err == nil {
		t.Fatal("a store failure was reported as a successful non-administrative answer")
	}

	missing := NewWebAuthnAdminResolver(&stubAttestUserResolver{users: map[string]*User{}}, stubAttestRoles(), nil)
	if _, err := missing.IsAdministrativeAccount(context.Background(), "t1", "ghost@example.com"); err == nil {
		t.Fatal("an unknown account was reported as a successful non-administrative answer")
	}
}

// TestNewWebAuthnAdminResolver_NilStore returns a nil resolver, which the gate
// treats as "cannot tell" and therefore as administrative.
func TestNewWebAuthnAdminResolver_NilStore(t *testing.T) {
	if r := NewWebAuthnAdminResolver(nil, stubAttestRoles(), nil); r != nil {
		t.Fatalf("NewWebAuthnAdminResolver(nil, stubAttestRoles(), nil) = %v, want nil", r)
	}
	if r := NewWebAuthnAdminResolver(&stubAttestUserResolver{}, nil, nil); r != nil {
		t.Fatalf("NewWebAuthnAdminResolver(userResolver, nil, nil) = %v, want nil", r)
	}
}

// TestWebAuthnAssurance_String keeps the policy printable in the startup log
// line an operator reads to confirm what the deployment is enforcing.
func TestWebAuthnAssurance_String(t *testing.T) {
	for assurance, want := range map[WebAuthnAssurance]string{
		WebAuthnAssuranceHardware:                            "hardware",
		WebAuthnAssuranceAny:                                 "any",
		WebAuthnAssuranceInsecureAllowSoftwareAdministrators: "insecure-allow-software-administrators",
		WebAuthnAssurance(99):                                "unknown",
	} {
		if got := assurance.String(); got != want {
			t.Errorf("WebAuthnAssurance(%d).String() = %q, want %q", assurance, got, want)
		}
	}
}

// TestWebAuthnAssuranceValueError_Message names the offending value and the
// accepted set, and renders on a nil receiver.
func TestWebAuthnAssuranceValueError_Message(t *testing.T) {
	msg := (&WebAuthnAssuranceValueError{Value: "hardwar"}).Error()
	if !strings.Contains(msg, "hardwar") || !strings.Contains(msg, "\"any\"") {
		t.Errorf("unexpected message: %s", msg)
	}
	var nilErr *WebAuthnAssuranceValueError
	if nilErr.Error() == "" {
		t.Error("nil WebAuthnAssuranceValueError produced an empty message")
	}
}

// TestNormalizedAttestationFormat reports an absent attestation type as "none"
// so the stored factor and the refusal message name the same thing the WebAuthn
// spec does, rather than an empty string.
func TestNormalizedAttestationFormat(t *testing.T) {
	for in, want := range map[string]string{
		"":            "none",
		"   ":         "none",
		"BASIC_FULL":  "basic_full",
		" basic_full": "basic_full",
	} {
		if got := normalizedAttestationFormat(in); got != want {
			t.Errorf("normalizedAttestationFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAdminRoleResolver_NilReceiver fails closed rather than panicking.
func TestAdminRoleResolver_NilReceiver(t *testing.T) {
	var resolver *adminRoleResolver
	if _, err := resolver.IsAdministrativeAccount(context.Background(), "t1", "a@b.c"); err == nil {
		t.Fatal("a nil resolver answered successfully")
	}
	empty := &adminRoleResolver{}
	if _, err := empty.IsAdministrativeAccount(context.Background(), "t1", "a@b.c"); err == nil {
		t.Fatal("a resolver with no store answered successfully")
	}
}

// TestAdminRoleResolver_NilUserIsAnError covers the store that answers
// (nil, nil) — an absent user must not read as "not an administrator".
func TestAdminRoleResolver_NilUserIsAnError(t *testing.T) {
	resolver := NewWebAuthnAdminResolver(&stubAttestNilUserResolver{}, stubAttestRoles(), nil)
	if _, err := resolver.IsAdministrativeAccount(context.Background(), "t1", "a@b.c"); err == nil {
		t.Fatal("a nil user was reported as a successful non-administrative answer")
	}
}

// stubAttestNilUserResolver returns (nil, nil) — the shape a store can produce
// for a missing record without treating it as an error.
type stubAttestNilUserResolver struct{}

func (stubAttestNilUserResolver) GetUserByEmail(context.Context, string, string) (*User, error) {
	return nil, nil
}

// -----------------------------------------------------------------------------
// The test-environment opt-in
// -----------------------------------------------------------------------------

// TestWebAuthnAttestationGate_InsecurePolicyAdmitsSoftwareAdministrators is the
// escape hatch working as designed: an automated test environment can enroll an
// administrator with a headless software authenticator.
//
// It is deliberately the ONLY policy that does so, and the credential still
// records what it actually proved, so even here nothing is represented as
// hardware that is not.
func TestWebAuthnAttestationGate_InsecurePolicyAdmitsSoftwareAdministrators(t *testing.T) {
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceInsecureAllowSoftwareAdministrators,
		nil, // not even an allowlist is needed: hardware is never required
		&stubAdminResolver{admins: map[string]bool{"admin@example.com": true}},
	)

	result, err := gate.Evaluate(context.Background(), "t1", "admin@example.com", "none", virtualAAGUID)
	if err != nil {
		t.Fatalf("the insecure test policy refused a software administrator: %v", err)
	}
	if result.Level() != AttestLevelNone {
		t.Errorf("Level = %q, want %q — the record must still say what was proven",
			result.Level(), AttestLevelNone)
	}
	if result.IsHardware() {
		t.Fatal("a software credential admitted by the insecure policy reported hardware")
	}
}

// TestWebAuthnAssurance_AllowsSoftwareAdministrators pins the predicate the
// startup path uses to decide whether to announce the node as insecure. Only
// one value may answer true.
func TestWebAuthnAssurance_AllowsSoftwareAdministrators(t *testing.T) {
	if !WebAuthnAssuranceInsecureAllowSoftwareAdministrators.AllowsSoftwareAdministrators() {
		t.Error("the insecure policy does not report itself as such")
	}
	for _, assurance := range []WebAuthnAssurance{
		WebAuthnAssuranceHardware,
		WebAuthnAssuranceAny,
		WebAuthnAssurance(99),
	} {
		if assurance.AllowsSoftwareAdministrators() {
			t.Errorf("%v reported that it allows software administrators", assurance)
		}
	}
}

// TestParseWebAuthnAssurance_NoShortAliasForTheInsecurePolicy is the guard on
// how the escape hatch is spelled. The only value that selects it is the one
// that says what it does — a shorter alias would make it reachable by someone
// who had not read what they were turning off.
func TestParseWebAuthnAssurance_NoShortAliasForTheInsecurePolicy(t *testing.T) {
	for _, alias := range []string{"insecure", "insecure-any", "allow-software-administrators", "software", "test"} {
		got, err := ParseWebAuthnAssurance(alias)
		if err == nil {
			t.Errorf("ParseWebAuthnAssurance(%q) was accepted as an alias", alias)
		}
		if got.AllowsSoftwareAdministrators() {
			t.Errorf("ParseWebAuthnAssurance(%q) resolved to the insecure policy", alias)
		}
	}
}

// -----------------------------------------------------------------------------
// The attestation trust path
// -----------------------------------------------------------------------------

// TestWebAuthnAttestationGate_RefusesUnverifiedAttestationChain is the control
// that stops the whole scheme from being defeated one level down.
//
// go-webauthn reports AttestationType "basic_full" after checking only that the
// statement is internally consistent — it explicitly does not walk the
// certificate chain ("We don't handle trust paths yet but we're done"), and it
// returns before any metadata validation when no MDS provider is supplied,
// which is every ceremony this platform runs. So an attacker who generates
// their own attestation certificate, puts an ALLOWLISTED AAGUID in both the
// certificate extension and the authenticator data, and signs the statement
// with it, satisfies both "basic_full" and the allowlist using material they
// chose.
//
// Hardware therefore additionally requires a trust path verified against roots
// the operator configured. No verified path on the context means no hardware.
func TestWebAuthnAttestationGate_RefusesUnverifiedAttestationChain(t *testing.T) {
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)

	// A perfectly-formed forgery: manufacturer-grade attestation type, an
	// allowlisted model, and no verified chain behind either.
	result, err := gate.Evaluate(context.Background(), "t1", "admin@example.com", "basic_full", yubiKey5AAGUID)
	if err == nil {
		t.Fatal("an attestation with no verified trust path was accepted as hardware")
	}
	if result != nil {
		t.Fatalf("a refusal must return no result, got %+v", result)
	}

	var unverified *AttestationChainUnverifiedError
	if !errors.As(err, &unverified) {
		t.Fatalf("want *AttestationChainUnverifiedError, got %T: %v", err, err)
	}
	if msg := err.Error(); !strings.Contains(msg, "attestation_roots_file") {
		t.Errorf("the refusal must name the missing setting: %q", msg)
	}
}

// TestWebAuthnAttestationGate_RefusesChainForADifferentModel binds the verified
// chain to THIS credential. Without the binding, a chain verified for one
// authenticator could vouch for a credential reporting a different model.
func TestWebAuthnAttestationGate_RefusesChainForADifferentModel(t *testing.T) {
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)

	other := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}

	// The chain vouches for `other`; the credential claims the allowlisted model.
	_, err := gate.Evaluate(verifiedChainContext(other), "t1", "admin@example.com", "basic_full", yubiKey5AAGUID)
	if err == nil {
		t.Fatal("a chain verified for a different authenticator model was accepted")
	}
	var mismatch *AttestationChainMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("want *AttestationChainMismatchError, got %T: %v", err, err)
	}
}

// TestWebAuthnAttestationGate_UnverifiedChainDoesNotBlockPermissiveTenants
// confirms the trust-path requirement applies only where hardware is required.
// A tenant user under the permissive policy still enrolls, and still records
// what was proven.
func TestWebAuthnAttestationGate_UnverifiedChainDoesNotBlockPermissiveTenants(t *testing.T) {
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{"admin@example.com": true}},
	)

	result, err := gate.Evaluate(context.Background(), "t1", "user@example.com", "basic_full", yubiKey5AAGUID)
	if err != nil {
		t.Fatalf("a tenant user was refused for an unverified chain: %v", err)
	}
	// The tenant user is ADMITTED — and RECORDED as software.
	//
	// This is the half that is easy to get wrong. Capping the level only at the
	// admin gate would let this enrollment persist attest_level=hardware for an
	// authenticator nothing vouched for, and login policy reads that record. A
	// credential must never be described as more than was proven, whichever
	// policy admitted it.
	if result.Level() != AttestLevelSoftware {
		t.Errorf("Level = %q, want %q — an unverified basic_full proves possession, not storage",
			result.Level(), AttestLevelSoftware)
	}
	if result.IsHardware() {
		t.Fatal("an unverified attestation was recorded as hardware")
	}
	// The FORMAT still records what the authenticator claimed, so an operator
	// can see the difference between "said basic_full, unproven" and "said
	// nothing".
	if result.Format() != "basic_full" {
		t.Errorf("Format = %q, want the claimed basic_full", result.Format())
	}
}

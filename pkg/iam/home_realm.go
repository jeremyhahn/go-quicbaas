//go:build iam

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

package iam

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Home-realm discovery (HRD) key prefixes. HRD is a SECRET-FREE SHARED ROUTING
// DIRECTORY: it maps a domain / SNI host to a tenant ID and NOTHING else (no
// users, secrets, or PII). It lives in the shared control-plane/system store
// so it is readable BEFORE any target tenant's barrier unseals — this is what
// lets a browser/WebAuthn/federated login (which presents no tenant-scoped
// SPIFFE SAN) discover its tenant without a chicken-and-egg dependency on the
// very barrier it is trying to reach.
const (
	// KeyPrefixHRDDomain maps an email/SNI domain to a tenant ID.
	KeyPrefixHRDDomain = KeyPrefixIAM + "hrd/domain/"

	// KeyPrefixHRDHost maps an exact SNI/Host header value to a tenant ID.
	KeyPrefixHRDHost = KeyPrefixIAM + "hrd/host/"
)

// hrdDomainKey builds the routing key for an email/SNI domain.
func hrdDomainKey(domain string) string {
	return KeyPrefixHRDDomain + strings.ToLower(strings.TrimSpace(domain))
}

// hrdHostKey builds the routing key for an exact SNI/Host value.
func hrdHostKey(host string) string {
	return KeyPrefixHRDHost + strings.ToLower(strings.TrimSpace(host))
}

// RealmHints carries the non-secret signals a login presents for home-realm
// discovery. Resolution order is: explicit tenant hint -> SNI/Host -> email
// domain -> fail. None of these values is trusted as the tenant directly; they
// are looked up in the shared routing directory (except the explicit hint,
// which is validated against a registered entry to avoid spoofing).
type RealmHints struct {
	// TenantHint is an explicit tenant the caller claims (e.g. an X-Tenant
	// header or a subdomain already resolved upstream). It is accepted only
	// when it matches a registered routing entry, never blindly.
	TenantHint string

	// SNIHost is the TLS SNI or HTTP Host the client connected to.
	SNIHost string

	// Email is the subject email; its domain is used for routing.
	Email string
}

// HomeRealmResolver resolves a login's tenant from non-secret routing hints.
type HomeRealmResolver interface {
	// Resolve returns the tenant ID for the given hints, or an error when no
	// routing entry matches (fail-closed; the caller must reject the login).
	Resolve(ctx context.Context, hints RealmHints) (string, error)
}

// HomeRealmDirectory is the secret-free shared routing directory backing
// home-realm discovery and the tenant self-service register/unregister API. It
// operates DIRECTLY on the shared/system KVStore (NOT the per-tenant routing
// store) so entries are written to and read from the control-plane store and
// remain readable while target tenant barriers are sealed.
//
// A tenant admin registers the domains/hosts it owns; the entries are stored
// in the shared store keyed by domain/host -> tenantID. The directory NEVER
// stores user records, secrets, or PII.
type HomeRealmDirectory struct {
	// shared is the control-plane/system store. It MUST be the shared store,
	// not a tenant-barrier-routed store, so HRD reads succeed pre-unseal.
	shared interfaces.KVStore
}

var _ HomeRealmResolver = (*HomeRealmDirectory)(nil)

// NewHomeRealmDirectory builds a directory over the shared/system KVStore.
func NewHomeRealmDirectory(shared interfaces.KVStore) (*HomeRealmDirectory, error) {
	if shared == nil {
		return nil, &HRDInvalidError{Reason: "shared store must not be nil"}
	}
	return &HomeRealmDirectory{shared: shared}, nil
}

// RegisterDomain binds an email/SNI domain to a tenant. This is the tenant
// self-service write path; it writes ONLY to the shared store. A domain may be
// bound to exactly one tenant: re-registering a domain already owned by a
// different tenant is rejected to prevent tenant hijacking.
func (d *HomeRealmDirectory) RegisterDomain(ctx context.Context, tenantID, domain string) error {
	if tenantID == "" || strings.TrimSpace(domain) == "" {
		return &HRDInvalidError{Reason: "tenant and domain are required"}
	}
	key := hrdDomainKey(domain)
	if err := d.guardOwnership(ctx, key, tenantID); err != nil {
		return err
	}
	return d.shared.Put(ctx, key, []byte(tenantID))
}

// RegisterHost binds an exact SNI/Host value to a tenant (self-service).
func (d *HomeRealmDirectory) RegisterHost(ctx context.Context, tenantID, host string) error {
	if tenantID == "" || strings.TrimSpace(host) == "" {
		return &HRDInvalidError{Reason: "tenant and host are required"}
	}
	key := hrdHostKey(host)
	if err := d.guardOwnership(ctx, key, tenantID); err != nil {
		return err
	}
	return d.shared.Put(ctx, key, []byte(tenantID))
}

// UnregisterDomain removes a domain routing entry. Only the owning tenant may
// remove its own entry.
func (d *HomeRealmDirectory) UnregisterDomain(ctx context.Context, tenantID, domain string) error {
	key := hrdDomainKey(domain)
	if err := d.requireOwner(ctx, key, tenantID); err != nil {
		return err
	}
	return d.shared.Delete(ctx, key)
}

// UnregisterHost removes a host routing entry. Only the owning tenant may
// remove its own entry.
func (d *HomeRealmDirectory) UnregisterHost(ctx context.Context, tenantID, host string) error {
	key := hrdHostKey(host)
	if err := d.requireOwner(ctx, key, tenantID); err != nil {
		return err
	}
	return d.shared.Delete(ctx, key)
}

// Resolve implements HomeRealmResolver. Order: explicit tenant hint (validated
// against a registered entry) -> SNI/Host -> email domain -> fail closed.
func (d *HomeRealmDirectory) Resolve(ctx context.Context, hints RealmHints) (string, error) {
	// 1. Explicit hint: accept only if it owns at least one matching routing
	//    entry, so a caller cannot assert an arbitrary tenant.
	if hints.TenantHint != "" {
		if tenantID, ok := d.validateHint(ctx, hints); ok {
			return tenantID, nil
		}
	}

	// 2. Exact SNI/Host.
	if hints.SNIHost != "" {
		if tenantID, ok := d.lookup(ctx, hrdHostKey(hints.SNIHost)); ok {
			return tenantID, nil
		}
		// Fall back to the host's registrable domain (strip the leftmost label
		// for subdomain-per-tenant deployments).
		if domain := parentDomain(hints.SNIHost); domain != "" {
			if tenantID, ok := d.lookup(ctx, hrdDomainKey(domain)); ok {
				return tenantID, nil
			}
		}
	}

	// 3. Email domain.
	if hints.Email != "" {
		if at := strings.LastIndex(hints.Email, "@"); at >= 0 && at < len(hints.Email)-1 {
			domain := hints.Email[at+1:]
			if tenantID, ok := d.lookup(ctx, hrdDomainKey(domain)); ok {
				return tenantID, nil
			}
		}
	}

	// 4. Fail closed — never default to the system tenant for a non-bootstrap
	//    login.
	return "", &HRDNotFoundError{}
}

// validateHint reports the tenant when the explicit hint matches a registered
// host or email-domain routing entry that the hinted tenant owns.
func (d *HomeRealmDirectory) validateHint(ctx context.Context, hints RealmHints) (string, bool) {
	if hints.SNIHost != "" {
		if tenantID, ok := d.lookup(ctx, hrdHostKey(hints.SNIHost)); ok && tenantID == hints.TenantHint {
			return tenantID, true
		}
		if domain := parentDomain(hints.SNIHost); domain != "" {
			if tenantID, ok := d.lookup(ctx, hrdDomainKey(domain)); ok && tenantID == hints.TenantHint {
				return tenantID, true
			}
		}
	}
	if hints.Email != "" {
		if at := strings.LastIndex(hints.Email, "@"); at >= 0 && at < len(hints.Email)-1 {
			if tenantID, ok := d.lookup(ctx, hrdDomainKey(hints.Email[at+1:])); ok && tenantID == hints.TenantHint {
				return tenantID, true
			}
		}
	}
	return "", false
}

// lookup reads a routing key from the shared store, returning the tenant ID and
// whether the entry exists. A missing key is not an error.
func (d *HomeRealmDirectory) lookup(ctx context.Context, key string) (string, bool) {
	data, err := d.shared.Get(ctx, key)
	if err != nil {
		// Treat not-found as a clean miss; any other error is also a miss so a
		// transient shared-store error never silently routes to a wrong tenant.
		return "", false
	}
	if len(data) == 0 {
		return "", false
	}
	return string(data), true
}

// guardOwnership rejects writing a routing entry already owned by a different
// tenant.
func (d *HomeRealmDirectory) guardOwnership(ctx context.Context, key, tenantID string) error {
	if existing, ok := d.lookup(ctx, key); ok && existing != tenantID {
		return &HRDConflictError{Owner: existing}
	}
	return nil
}

// requireOwner rejects an unregister by a tenant that does not own the entry.
func (d *HomeRealmDirectory) requireOwner(ctx context.Context, key, tenantID string) error {
	existing, ok := d.lookup(ctx, key)
	if !ok {
		return &HRDNotFoundError{}
	}
	if existing != tenantID {
		return &HRDConflictError{Owner: existing}
	}
	return nil
}

// parentDomain strips the leftmost DNS label, e.g. "acme.example.com" ->
// "example.com". Returns "" when there is no parent label.
func parentDomain(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i] // drop any :port
	}
	idx := strings.Index(host, ".")
	if idx < 0 || idx == len(host)-1 {
		return ""
	}
	parent := host[idx+1:]
	if !strings.Contains(parent, ".") {
		// A single-label parent (e.g. "com") is not a routable domain.
		return ""
	}
	return parent
}

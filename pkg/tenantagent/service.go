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

package tenantagent

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/policy/xkmssign"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// IdentityEnroller is the go-qrdb principal surface; *iam.IdentityStore satisfies it.
type IdentityEnroller interface {
	EnrollTenantAgentIdentity(ctx context.Context, tenantID string, cert *x509.Certificate) (*iam.User, error)
	RevokeTenantAgentIdentity(ctx context.Context, tenantID, username string) error
}

// Compile-time interface satisfaction check.
var _ IdentityEnroller = (*iam.IdentityStore)(nil)

// HashAlgorithm is the digest every Tenant Agent signature is made over.
const HashAlgorithm = xkmssign.HashAlgorithm

// SigningKeyFor returns the go-xkms key ID tenantID's Tenant Agent signs with.
func SigningKeyFor(tenantID string) string { return "tenant-agent-signing-" + tenantID }

var (
	// ErrNotConfigured reports a Service built without a required dependency.
	ErrNotConfigured = errors.New("tenantagent: service is not configured")

	// ErrTenantRequired reports a call that named no tenant.
	ErrTenantRequired = errors.New("tenantagent: tenant is required")

	// ErrWrongTenant reports a call whose context is scoped to a tenant other
	// than the one it names.
	ErrWrongTenant = errors.New("tenantagent: context is scoped to another tenant")

	// ErrNotLeadCapable reports an appointment of an Agent, or a pool member,
	// that does not declare the lead role.
	ErrNotLeadCapable = errors.New("tenantagent: agent does not declare the lead role")

	// ErrNotAppointed reports a tenant with no Tenant Agent.
	ErrNotAppointed = errors.New("tenantagent: no Tenant Agent is appointed")

	// ErrForeignKey reports a verification against a key that is not the
	// tenant's Tenant Agent key.
	ErrForeignKey = errors.New("tenantagent: key is not this tenant's Tenant Agent key")

	// ErrBadSignature reports a signature that does not verify.
	ErrBadSignature = errors.New("tenantagent: signature does not verify")
)

// Options are the Service's dependencies; every one is required.
type Options struct {
	Catalog  orchestration.AgentCatalogStore
	Store    orchestration.TenantAgentStore
	Identity IdentityEnroller
	Keys     xkmssign.KeyOps
}

// Service appoints, retires and signs as a tenant's Tenant Agent.
type Service struct {
	catalog  orchestration.AgentCatalogStore
	store    orchestration.TenantAgentStore
	identity IdentityEnroller
	keys     xkmssign.KeyOps
}

// New returns a Service, or an error wrapping ErrNotConfigured naming the
// first missing dependency.
func New(opts Options) (*Service, error) {
	switch {
	case opts.Catalog == nil:
		return nil, fmt.Errorf("%w: Catalog is nil", ErrNotConfigured)
	case opts.Store == nil:
		return nil, fmt.Errorf("%w: Store is nil", ErrNotConfigured)
	case opts.Identity == nil:
		return nil, fmt.Errorf("%w: Identity is nil", ErrNotConfigured)
	case opts.Keys == nil:
		return nil, fmt.Errorf("%w: Keys is nil", ErrNotConfigured)
	}
	return &Service{catalog: opts.Catalog, store: opts.Store, identity: opts.Identity, keys: opts.Keys}, nil
}

// scope names the one tenant a call acts on. It refuses an unnamed tenant and
// a context already scoped to another, and returns ctx scoped to the tenant
// for both the orchestration stores and go-qrdb.
func (s *Service) scope(ctx context.Context, tenantID string) (context.Context, string, error) {
	tenant := strings.TrimSpace(tenantID)
	if tenant == "" {
		return nil, "", ErrTenantRequired
	}
	if scoped := orchestration.TenantFromContext(ctx); scoped != "" && scoped != tenant {
		return nil, "", fmt.Errorf("%w: context names %q, call names %q", ErrWrongTenant, scoped, tenant)
	}
	if scoped, ok := tenantctx.Lookup(ctx); ok && scoped != "" && scoped != tenant {
		return nil, "", fmt.Errorf("%w: context names %q, call names %q", ErrWrongTenant, scoped, tenant)
	}
	ctx = orchestration.ContextWithTenant(ctx, tenant)
	ctx = tenantctx.WithTenant(ctx, tenant)
	return ctx, tenant, nil
}

// AppointRequest names what a Tenant Agent appointment binds: the catalog
// Agent or pool that runs it and the certificate its principal is named by.
type AppointRequest struct {
	Agent       orchestration.AgentRef
	Certificate *x509.Certificate
}

// Appoint makes the requested Agent or pool tenantID's Tenant Agent. It
// enrolls the certificate's go-qrdb principal with the Tenant Agent role and
// stores the appointment. A tenant has one Tenant Agent: appointing a second
// principal is refused (*orchestration.TenantAgentExistsError) before anything
// is enrolled, while re-appointing the standing principal converges it and
// rebinds the Agent ref.
func (s *Service) Appoint(ctx context.Context, tenantID string, req AppointRequest) (*orchestration.TenantAgent, error) {
	ctx, tenant, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if err := orchestration.RequireAgentRef(req.Agent, "agent"); err != nil {
		return nil, err
	}
	if req.Certificate == nil {
		return nil, &orchestration.InputValidationError{Field: "certificate", Message: "required"}
	}
	if err := s.requireLead(ctx, tenant, req.Agent); err != nil {
		return nil, err
	}

	reappoint := false
	held, err := s.store.GetTenantAgent(ctx, tenant)
	switch {
	case err == nil:
		if held.Principal != strings.TrimSpace(req.Certificate.Subject.CommonName) {
			return nil, &orchestration.TenantAgentExistsError{TenantID: tenant, Principal: held.Principal}
		}
		reappoint = true
	case !errors.Is(err, orchestration.ErrTenantAgentUnknown):
		return nil, err
	}

	user, err := s.identity.EnrollTenantAgentIdentity(ctx, tenant, req.Certificate)
	if err != nil {
		return nil, err
	}
	record := &orchestration.TenantAgent{
		Agent:        req.Agent,
		Principal:    user.Username,
		SigningKeyID: SigningKeyFor(tenant),
	}
	if reappoint {
		// The principal was already the standing one, so a failed update
		// leaves it as it was and nothing is revoked.
		if err := s.store.UpdateTenantAgent(ctx, tenant, record); err != nil {
			return nil, err
		}
	} else if err := s.store.CreateTenantAgent(ctx, tenant, record); err != nil {
		// Revoke the fresh principal so a lost race or failed write never
		// leaves one standing without an appointment.
		return nil, errors.Join(err, s.identity.RevokeTenantAgentIdentity(ctx, tenant, user.Username))
	}
	return s.store.GetTenantAgent(ctx, tenant)
}

// requireLead refuses ref unless it names an Agent declaring the lead role,
// or a pool every member of which does: a pool fails over to any member, so
// any member may end up acting. Catalog errors are returned unchanged.
func (s *Service) requireLead(ctx context.Context, tenant string, ref orchestration.AgentRef) error {
	ids := []string{ref.AgentID}
	if ref.PoolID != "" {
		pool, err := s.catalog.GetPool(ctx, tenant, ref.PoolID)
		if err != nil {
			return err
		}
		ids = pool.Agents
	}
	for _, id := range ids {
		agent, err := s.catalog.GetAgent(ctx, tenant, id)
		if err != nil {
			return err
		}
		if !agent.HasRole(orchestration.AgentRoleLead) {
			return fmt.Errorf("%w: agent %q", ErrNotLeadCapable, id)
		}
	}
	return nil
}

// Get returns tenantID's Tenant Agent appointment.
func (s *Service) Get(ctx context.Context, tenantID string) (*orchestration.TenantAgent, error) {
	ctx, tenant, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.store.GetTenantAgent(ctx, tenant)
}

// Retire revokes tenantID's Tenant Agent principal and removes the
// appointment. It is idempotent. The principal is revoked first, so a failure
// leaves the record in place and a retry finishes the job.
func (s *Service) Retire(ctx context.Context, tenantID string) error {
	ctx, tenant, err := s.scope(ctx, tenantID)
	if err != nil {
		return err
	}
	held, err := s.store.GetTenantAgent(ctx, tenant)
	if err != nil {
		if errors.Is(err, orchestration.ErrTenantAgentUnknown) {
			return nil
		}
		return err
	}
	if err := s.identity.RevokeTenantAgentIdentity(ctx, tenant, held.Principal); err != nil {
		return err
	}
	return s.store.DeleteTenantAgent(ctx, tenant)
}

// Sign signs payload with tenantID's Tenant Agent key and returns the key ID
// with the signature.
func (s *Service) Sign(ctx context.Context, tenantID string, payload []byte) (keyID string, sig []byte, err error) {
	ctx, tenant, err := s.scope(ctx, tenantID)
	if err != nil {
		return "", nil, err
	}
	held, err := s.store.GetTenantAgent(ctx, tenant)
	if err != nil {
		if errors.Is(err, orchestration.ErrTenantAgentUnknown) {
			return "", nil, fmt.Errorf("%w: %w", ErrNotAppointed, err)
		}
		return "", nil, err
	}
	sig, err = s.keys.Sign(ctx, held.SigningKeyID, payload, HashAlgorithm, false)
	if err != nil {
		return "", nil, fmt.Errorf("tenantagent: sign with %q: %w", held.SigningKeyID, err)
	}
	return held.SigningKeyID, sig, nil
}

// Verify checks that sig is tenantID's Tenant Agent signature over payload.
// A key other than the tenant's Tenant Agent key is refused before any key
// operation.
func (s *Service) Verify(ctx context.Context, tenantID, keyID string, payload, sig []byte) error {
	ctx, tenant, err := s.scope(ctx, tenantID)
	if err != nil {
		return err
	}
	if keyID != SigningKeyFor(tenant) {
		return fmt.Errorf("%w: %q", ErrForeignKey, keyID)
	}
	ok, err := s.keys.Verify(ctx, keyID, payload, sig, HashAlgorithm)
	if err != nil {
		return fmt.Errorf("tenantagent: verify with %q: %w", keyID, err)
	}
	if !ok {
		return ErrBadSignature
	}
	return nil
}

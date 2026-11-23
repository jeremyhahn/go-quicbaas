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

// Package grants holds the five grants deploy makes to a service, one
// serviceplane.Grant per kind, each over the subsystem that owns it:
//
//   - identity: an X.509-SVID from the go-xkms CA under the x509-svid
//     profile, spiffe://<trust domain>/tenant/<tenant>/svc/<name>, recorded
//     as the tenant's;
//   - certificate: a TLS leaf for the registered name from the same CA under
//     the ACME leaf profile, the profile go-xkms ACME issues under;
//   - name: a registration in pkg/registry;
//   - rbac: the manifest's built-in role bound to the workload identity;
//   - secrets: the service's secret namespace, bound to the workload
//     identity.
//
// The SVID and the TLS leaf, with their private keys, are kept in the secret
// store under the service's namespace, where the launcher reads them. Every
// grant is an ensure: given the reference a previous deploy made, it keeps it.
// Each Revoke undoes its own Grant and tolerates a reference already gone.
package grants

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// reasonCessation is RFC 5280 CRLReason cessationOfOperation, the reason an
// undeployed service's certificates are revoked with.
const reasonCessation = 5

// CA is the slice of the go-xkms CA the identity and certificate grants
// use. xkmssdk.TrustedCA satisfies it.
type CA interface {
	SignCSR(ctx context.Context, csrPEM []byte, opts *xkmssdk.TrustedCASignOptions) (*x509.Certificate, error)
	Revoke(ctx context.Context, serial *big.Int, reason int) error
}

// SecretStore is the slice of the secrets manager the grants use.
// *secrets.Manager satisfies it.
type SecretStore interface {
	Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error
	Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error)
	Destroy(ctx context.Context, path string) error
}

// NameRegistry is the slice of pkg/registry the name grant uses.
// *registry.Registry satisfies it.
type NameRegistry interface {
	Register(ctx context.Context, inst registry.ServiceInstance, ttl time.Duration) (int64, error)
	Deregister(ctx context.Context, leaseID int64) error
	Renew(ctx context.Context, leaseID int64) (int64, error)
}

// RoleBinder binds a built-in role to a principal within a tenant. svid is
// the principal's current SVID, the certificate it logs in with.
// iam.WorkloadRoleBinder satisfies it.
type RoleBinder interface {
	AssignRole(ctx context.Context, tenantID, principal, role string, svid *x509.Certificate) error
	RevokeRole(ctx context.Context, tenantID, principal, role string) error
}

// SPIFFEID is the workload identity of a service, in the platform's
// tenant-scoped form spiffe://<trust domain>/tenant/<tenant>/svc/<name>: the
// form the go-xkms CA stamps and go-qrdb's mTLS login derives the tenant from,
// so the service authenticates with its SVID as itself, in its own tenant.
// The trust domain is the operator's (iam.spiffe_trust_domain).
func SPIFFEID(trustDomain string, m *serviceplane.Manifest) string {
	return "spiffe://" + trustDomain + "/tenant/" + m.Tenant + "/" + qrdbsdk.SpiffeKindService + "/" + m.Name
}

// workloadID answers the service's SPIFFE ID, refusing a grant built without
// a trust domain.
func workloadID(kind serviceplane.GrantKind, trustDomain string, m *serviceplane.Manifest) (string, error) {
	if trustDomain == "" {
		return "", &ConfigError{Kind: kind, Reason: "no SPIFFE trust domain"}
	}
	return SPIFFEID(trustDomain, m), nil
}

// SecretPrefix is the service's secret namespace, relative to its tenant:
// the namespace the tenant sees through the secrets service.
func SecretPrefix(m *serviceplane.Manifest) string {
	return "services/" + m.Name + "/"
}

// secretPath is where the plane stores rel, a path in the tenant's
// namespace: under the tenant's scope, tenant/<tenant>/<rel>, the same place
// the secrets service maps the tenant's own rel to. The secret store uses a
// path verbatim, so without the scope two tenants' services of one name
// would share their credentials.
func secretPath(m *serviceplane.Manifest, rel string) string {
	if qrdbsdk.IsSystemTenant(m.Tenant) {
		return rel
	}
	return "tenant/" + m.Tenant + "/" + rel
}

// Paths of the credentials the grants keep for the launcher.
func svidPath(m *serviceplane.Manifest) string { return secretPath(m, SecretPrefix(m)+"svid") }
func tlsPath(m *serviceplane.Manifest) string  { return secretPath(m, SecretPrefix(m)+"tls") }
func bindingPath(m *serviceplane.Manifest) string {
	return secretPath(m, SecretPrefix(m)+"binding")
}

// tenantCtx binds the manifest's tenant, the scope every grant writes in.
func tenantCtx(ctx context.Context, m *serviceplane.Manifest) context.Context {
	return tenantctx.WithTenant(ctx, m.Tenant)
}

// ---------------------------------------------------------------------------
// identity

// Identity grants the workload identity, an X.509-SVID.
type Identity struct {
	CA      CA
	Secrets SecretStore
	// TrustDomain is the operator's SPIFFE trust domain.
	TrustDomain string
	// Validity is the SVID lifetime; zero takes the profile's default.
	Validity time.Duration
	// Now is the clock renewal is judged by; nil is time.Now.
	Now func() time.Time
}

// Kind answers identity.
func (*Identity) Kind() serviceplane.GrantKind { return serviceplane.GrantIdentity }

// Grant issues the SVID, recorded as the tenant's, and keeps it with its
// key. Its reference is <spiffe id>#<hex serial>; a prior reference for
// another identity, as one made under another trust domain, is replaced.
func (g *Identity) Grant(ctx context.Context, m *serviceplane.Manifest, prior string) (string, error) {
	id, err := workloadID(serviceplane.GrantIdentity, g.TrustDomain, m)
	if err != nil {
		return "", err
	}
	if held, ok := strings.CutPrefix(prior, id+"#"); ok {
		// Keep the SVID while it is the stored one and not yet due for
		// renewal; otherwise issue its successor, which replaces it in
		// the store, and revoke it below. The RBAC grant then rebinds the
		// principal to the new key.
		cert, err := storedSVID(tenantCtx(ctx, m), g.Secrets, m)
		now := time.Now
		if g.Now != nil {
			now = g.Now
		}
		if err == nil && svidFresh(cert, held, now()) {
			return prior, nil
		}
	}
	serial, err := issue(tenantCtx(ctx, m), g.CA, g.Secrets, svidPath(m), id, &xkmssdk.TrustedCASignOptions{
		Profile:  xkmssdk.ProfileNameX509SVID,
		Validity: g.Validity,
		TenantID: m.Tenant,
		SANS:     &xkmssdk.TrustedCASubjectAlternativeNames{URIs: []string{id}},
	})
	if err != nil {
		return "", fmt.Errorf("identity %s: %w", id, err)
	}
	// The new SVID took the prior one's place in the store; revoke the prior.
	if _, old, ok := strings.Cut(prior, "#"); ok {
		if err := revokeSerial(tenantCtx(ctx, m), g.CA, serviceplane.GrantIdentity, old); err != nil {
			return "", errors.Join(fmt.Errorf("identity %s: revoke the prior SVID: %w", id, err),
				revokeSerial(tenantCtx(ctx, m), g.CA, serviceplane.GrantIdentity, serial))
		}
	}
	return id + "#" + serial, nil
}

// Revoke revokes the SVID and drops it.
func (g *Identity) Revoke(ctx context.Context, m *serviceplane.Manifest, ref string) error {
	_, serial, ok := strings.Cut(ref, "#")
	if !ok {
		return &RefError{Kind: serviceplane.GrantIdentity, Ref: ref}
	}
	return withdraw(tenantCtx(ctx, m), g.CA, g.Secrets, svidPath(m), serviceplane.GrantIdentity, serial)
}

// ---------------------------------------------------------------------------
// certificate

// Certificate grants a TLS certificate for the registered name.
type Certificate struct {
	CA      CA
	Secrets SecretStore
	// Domain is the DNS zone service names live under: <name>.<tenant>.<Domain>.
	Domain string
	// Validity is the leaf lifetime; zero takes the profile's default.
	Validity time.Duration
	// Now is the clock renewal is judged by; nil is time.Now. A held leaf
	// is renewed once two thirds of its lifetime have passed.
	Now func() time.Time
}

// Kind answers certificate.
func (*Certificate) Kind() serviceplane.GrantKind { return serviceplane.GrantCertificate }

// DNSName is the name the certificate is issued for.
func (g *Certificate) DNSName(m *serviceplane.Manifest) string {
	return m.Name + "." + m.Tenant + "." + g.Domain
}

// Grant issues the leaf and keeps it with its key. Its reference is the hex
// serial.
func (g *Certificate) Grant(ctx context.Context, m *serviceplane.Manifest, prior string) (string, error) {
	if g.Domain == "" {
		return "", &ConfigError{Kind: serviceplane.GrantCertificate, Reason: "no domain"}
	}
	dns := g.DNSName(m)
	tctx := tenantCtx(ctx, m)
	if prior != "" {
		// Keep the leaf while it is the stored one, for this name, and
		// not yet due for renewal; otherwise issue its successor, which
		// replaces its key and certificate in the store, and revoke it.
		now := time.Now
		if g.Now != nil {
			now = g.Now
		}
		cert, err := storedCert(tctx, g.Secrets, tlsPath(m))
		if err == nil && svidFresh(cert, prior, now()) && cert.Subject.CommonName == dns {
			return prior, nil
		}
	}
	serial, err := issue(tctx, g.CA, g.Secrets, tlsPath(m), dns, &xkmssdk.TrustedCASignOptions{
		Profile:  xkmssdk.ProfileNameACMELeaf,
		Validity: g.Validity,
		TenantID: m.Tenant,
		SANS:     &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{dns}},
	})
	if err != nil {
		return "", fmt.Errorf("certificate %s: %w", dns, err)
	}
	if prior != "" && prior != serial {
		if err := revokeSerial(tctx, g.CA, serviceplane.GrantCertificate, prior); err != nil {
			return "", errors.Join(fmt.Errorf("certificate %s: revoke the prior leaf: %w", dns, err),
				revokeSerial(tctx, g.CA, serviceplane.GrantCertificate, serial))
		}
	}
	return serial, nil
}

// Revoke revokes the leaf and drops it.
func (g *Certificate) Revoke(ctx context.Context, m *serviceplane.Manifest, ref string) error {
	return withdraw(tenantCtx(ctx, m), g.CA, g.Secrets, tlsPath(m), serviceplane.GrantCertificate, ref)
}

// issue makes a P-256 key with the audited requester-key generator (the
// launcher holds the key, so it cannot stay in a go-xkms keystore), has
// go-xkms build the CSR and the CA sign it, and keeps the certificate and
// key at path. It answers the hex serial.
func issue(ctx context.Context, ca CA, store SecretStore, path, cn string, opts *xkmssdk.TrustedCASignOptions) (string, error) {
	key, _, err := dbxkms.GenerateRequesterKey(dbxkms.KeyAlgorithmECDSAP256)
	if err != nil {
		return "", err
	}
	csrPEM, err := xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{CommonName: cn},
	}, key)
	if err != nil {
		return "", err
	}
	cert, err := ca.SignCSR(ctx, csrPEM, opts)
	if err != nil {
		return "", err
	}
	serial := cert.SerialNumber.Text(16)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", errors.Join(err, ca.Revoke(ctx, cert.SerialNumber, reasonCessation))
	}
	data := map[string][]byte{
		"certificate": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}),
		"private_key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}),
	}
	if err := store.Store(ctx, path, data, map[string]string{"serial": serial}); err != nil {
		return "", errors.Join(err, ca.Revoke(ctx, cert.SerialNumber, reasonCessation))
	}
	return serial, nil
}

// withdraw revokes a serial and destroys the kept credential. A serial
// already revoked and a credential already gone are done, so a partial
// undeploy can be retried.
func withdraw(ctx context.Context, ca CA, store SecretStore, path string, kind serviceplane.GrantKind, hexSerial string) error {
	if _, ok := new(big.Int).SetString(hexSerial, 16); !ok {
		return &RefError{Kind: kind, Ref: hexSerial}
	}
	return errors.Join(revokeSerial(ctx, ca, kind, hexSerial), destroyAbsent(ctx, store, path))
}

// revokeSerial revokes the certificate with the hex serial. One the CA has
// already revoked is revoked.
func revokeSerial(ctx context.Context, ca CA, kind serviceplane.GrantKind, hexSerial string) error {
	serial, ok := new(big.Int).SetString(hexSerial, 16)
	if !ok {
		return &RefError{Kind: kind, Ref: hexSerial}
	}
	if err := ca.Revoke(ctx, serial, reasonCessation); err != nil && !errors.Is(err, xkmsacme.CAErrAlreadyRevoked) {
		return err
	}
	return nil
}

// destroyAbsent destroys the entry at path; one already absent is
// destroyed.
func destroyAbsent(ctx context.Context, store SecretStore, path string) error {
	if err := store.Destroy(ctx, path); err != nil && !qberrors.IsNotFound(err) {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// name

// Name registers the service's name in pkg/registry.
type Name struct {
	Registry NameRegistry
	// TTL is the registration lease; the node agent renews it while the
	// service runs. Below registry.MinTTL it is raised to MinTTL.
	TTL time.Duration
	// Domain is the zone the name's address is under, as Certificate.Domain.
	Domain string
	// TrustDomain is the operator's SPIFFE trust domain.
	TrustDomain string
}

// Kind answers name.
func (*Name) Kind() serviceplane.GrantKind { return serviceplane.GrantName }

// Grant registers the name at <name>.<tenant>.<domain>:<first port>. Its
// reference is <lease>@<address>. On a redeploy at the same address the
// prior lease is renewed, or registered anew when it has expired; at a
// new address the prior lease is deregistered first, so the name never
// resolves to the old port.
func (g *Name) Grant(ctx context.Context, m *serviceplane.Manifest, prior string) (string, error) {
	if g.Domain == "" {
		return "", &ConfigError{Kind: serviceplane.GrantName, Reason: "no domain"}
	}
	id, err := workloadID(serviceplane.GrantName, g.TrustDomain, m)
	if err != nil {
		return "", err
	}
	addr := m.Name + "." + m.Tenant + "." + g.Domain
	if len(m.Ports) > 0 {
		addr += ":" + strconv.Itoa(int(m.Ports[0].Port))
	}
	tctx := tenantCtx(ctx, m)
	stale := int64(0)
	if prior != "" {
		lease, priorAddr, err := parseNameRef(prior)
		if err != nil {
			return "", err
		}
		if priorAddr == addr {
			if _, err := g.Registry.Renew(tctx, lease); err == nil {
				return prior, nil
			}
		}
		// The lease expired or cannot be renewed, or the address moved:
		// register anew first and drop the old lease after, so a failed
		// registration leaves the prior one in force and discoverable.
		stale = lease
	}
	lease, err := g.Registry.Register(tctx, registry.ServiceInstance{
		ID: m.Name, Service: m.Name, Address: addr,
		Metadata: map[string]string{"spiffe_id": id},
	}, g.TTL)
	if err != nil {
		return "", fmt.Errorf("name %s: %w", m.Name, err)
	}
	if stale != 0 {
		// Best effort: the registration already moved to the new lease,
		// and the old one's TTL removes it in any case.
		_ = g.deregister(tctx, stale)
	}
	return strconv.FormatInt(lease, 10) + "@" + addr, nil
}

// deregister revokes a lease; one already expired is gone.
func (g *Name) deregister(ctx context.Context, lease int64) error {
	err := g.Registry.Deregister(ctx, lease)
	var gone *registry.LeaseNotFoundError
	if errors.As(err, &gone) {
		return nil
	}
	return err
}

// parseNameRef splits a name reference into its lease and address. A bare
// lease, as references were before they carried the address, has none.
func parseNameRef(ref string) (int64, string, error) {
	leasePart, addr, _ := strings.Cut(ref, "@")
	lease, err := strconv.ParseInt(leasePart, 10, 64)
	if err != nil {
		return 0, "", &RefError{Kind: serviceplane.GrantName, Ref: ref}
	}
	return lease, addr, nil
}

// Revoke deregisters the name. A lease already expired is gone.
func (g *Name) Revoke(ctx context.Context, m *serviceplane.Manifest, ref string) error {
	lease, _, err := parseNameRef(ref)
	if err != nil {
		return err
	}
	return g.deregister(tenantCtx(ctx, m), lease)
}

// ---------------------------------------------------------------------------
// rbac

// RBAC binds the manifest's built-in role to the workload identity.
type RBAC struct {
	Roles RoleBinder
	// Secrets is where the identity grant keeps the SVID the role's
	// principal logs in with.
	Secrets SecretStore
	// TrustDomain is the operator's SPIFFE trust domain.
	TrustDomain string
}

// Kind answers rbac.
func (*RBAC) Kind() serviceplane.GrantKind { return serviceplane.GrantRBAC }

// Grant binds the role. Its reference is <role>@<spiffe id>. A redeploy
// that changes the role binds the new one and unbinds the prior. The
// principal is bound with the SVID the identity grant kept, its login key.
func (g *RBAC) Grant(ctx context.Context, m *serviceplane.Manifest, prior string) (string, error) {
	id, err := workloadID(serviceplane.GrantRBAC, g.TrustDomain, m)
	if err != nil {
		return "", err
	}
	ref := m.Role + "@" + id
	ctx = tenantCtx(ctx, m)
	svid, err := g.svid(ctx, m)
	if err != nil {
		return "", fmt.Errorf("rbac %s: %w", ref, err)
	}
	// Assign even when the reference is unchanged: the identity grant may
	// have renewed the SVID, and the principal's certificate binding must
	// follow the new key. Assigning a held role rebinds it in place.
	if err := g.Roles.AssignRole(ctx, m.Tenant, id, m.Role, svid); err != nil {
		return "", fmt.Errorf("rbac %s: %w", ref, err)
	}
	if prior == ref {
		return ref, nil
	}
	if prior != "" {
		if err := g.revoke(ctx, m, prior); err != nil {
			return "", errors.Join(err, g.Roles.RevokeRole(ctx, m.Tenant, id, m.Role))
		}
	}
	return ref, nil
}

// svid reads the SVID the identity grant kept, the principal's login key.
func (g *RBAC) svid(ctx context.Context, m *serviceplane.Manifest) (*x509.Certificate, error) {
	if g.Secrets == nil {
		return nil, &ConfigError{Kind: serviceplane.GrantRBAC, Reason: "no secret store"}
	}
	return storedSVID(ctx, g.Secrets, m)
}

// storedSVID reads the certificate of the SVID the identity grant keeps for m.
func storedSVID(ctx context.Context, store SecretStore, m *serviceplane.Manifest) (*x509.Certificate, error) {
	cert, err := storedCert(ctx, store, svidPath(m))
	if err != nil {
		return nil, fmt.Errorf("read the SVID: %w", err)
	}
	return cert, nil
}

// storedCert reads the certificate issue stored at path.
func storedCert(ctx context.Context, store SecretStore, path string) (*x509.Certificate, error) {
	data, _, err := store.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data["certificate"])
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("no certificate PEM at %s", path)
	}
	if _, ok := data["private_key"]; !ok {
		return nil, fmt.Errorf("no private key at %s", path)
	}
	return x509.ParseCertificate(block.Bytes)
}

// svidFresh reports whether the stored SVID is the one serial names and
// has more than a third of its lifetime left: past that, the identity grant
// renews it, so a long-running service never holds an expired credential.
func svidFresh(cert *x509.Certificate, serial string, now time.Time) bool {
	if cert == nil || cert.SerialNumber == nil || cert.SerialNumber.Text(16) != serial {
		return false
	}
	life := cert.NotAfter.Sub(cert.NotBefore)
	return now.Before(cert.NotAfter.Add(-life / 3))
}

// Revoke unbinds the role.
func (g *RBAC) Revoke(ctx context.Context, m *serviceplane.Manifest, ref string) error {
	return g.revoke(tenantCtx(ctx, m), m, ref)
}

func (g *RBAC) revoke(ctx context.Context, m *serviceplane.Manifest, ref string) error {
	role, principal, ok := strings.Cut(ref, "@")
	if !ok {
		return &RefError{Kind: serviceplane.GrantRBAC, Ref: ref}
	}
	return g.Roles.RevokeRole(ctx, m.Tenant, principal, role)
}

// ---------------------------------------------------------------------------
// secrets

// Secrets binds the service's secret namespace to its workload identity:
// the binding record names the one principal that may read under it.
type Secrets struct {
	Secrets SecretStore
	// TrustDomain is the operator's SPIFFE trust domain.
	TrustDomain string
}

// Kind answers secrets.
func (*Secrets) Kind() serviceplane.GrantKind { return serviceplane.GrantSecrets }

// Grant writes the binding. Its reference is the namespace.
func (g *Secrets) Grant(ctx context.Context, m *serviceplane.Manifest, prior string) (string, error) {
	id, err := workloadID(serviceplane.GrantSecrets, g.TrustDomain, m)
	if err != nil {
		return "", err
	}
	tctx := tenantCtx(ctx, m)
	if prior == SecretPrefix(m) {
		// Keep the binding only while it is in the store, bound to this
		// identity: a revoked one (an undeploy that failed part way) is
		// bound anew.
		data, _, err := g.Secrets.Get(tctx, bindingPath(m))
		if err == nil && string(data["spiffe_id"]) == id {
			return prior, nil
		}
		if err != nil && !qberrors.IsNotFound(err) {
			return "", fmt.Errorf("secrets %s: %w", SecretPrefix(m), err)
		}
	}
	err = g.Secrets.Store(tctx, bindingPath(m),
		map[string][]byte{"spiffe_id": []byte(id)},
		map[string]string{"bound_to": id, "namespace": SecretPrefix(m)})
	if err != nil {
		return "", fmt.Errorf("secrets %s: %w", SecretPrefix(m), err)
	}
	return SecretPrefix(m), nil
}

// Revoke removes the binding. A binding already gone is removed.
func (g *Secrets) Revoke(ctx context.Context, m *serviceplane.Manifest, _ string) error {
	return destroyAbsent(tenantCtx(ctx, m), g.Secrets, bindingPath(m))
}

// ---------------------------------------------------------------------------

// Config is what All needs to build the five grants.
type Config struct {
	CA       CA
	Secrets  SecretStore
	Registry NameRegistry
	Roles    RoleBinder
	// Domain is the zone service names live under.
	Domain string
	// TrustDomain is the operator's SPIFFE trust domain, the one the go-xkms
	// CA issues under and go-qrdb login pins (iam.spiffe_trust_domain).
	TrustDomain string
	// SVIDValidity and NameTTL are optional.
	SVIDValidity time.Duration
	NameTTL      time.Duration
}

// All builds the five grants in deploy order. Every dependency is
// required.
func All(c Config) ([]serviceplane.Grant, error) {
	switch {
	case c.CA == nil:
		return nil, &ConfigError{Reason: "no CA"}
	case c.Secrets == nil:
		return nil, &ConfigError{Reason: "no secret store"}
	case c.Registry == nil:
		return nil, &ConfigError{Reason: "no registry"}
	case c.Roles == nil:
		return nil, &ConfigError{Reason: "no role binder"}
	case c.Domain == "":
		return nil, &ConfigError{Reason: "no domain"}
	}
	if err := validTrustDomain(c.TrustDomain); err != nil {
		return nil, err
	}
	td := c.TrustDomain
	return []serviceplane.Grant{
		&Identity{CA: c.CA, Secrets: c.Secrets, TrustDomain: td, Validity: c.SVIDValidity},
		&Certificate{CA: c.CA, Secrets: c.Secrets, Domain: c.Domain},
		&Name{Registry: c.Registry, TTL: c.NameTTL, Domain: c.Domain, TrustDomain: td},
		&RBAC{Roles: c.Roles, Secrets: c.Secrets, TrustDomain: td},
		&Secrets{Secrets: c.Secrets, TrustDomain: td},
	}, nil
}

// validTrustDomain refuses a trust domain a SPIFFE ID cannot carry as its
// bare host: empty, or one with a path, port, user or other part.
func validTrustDomain(td string) error {
	if td == "" {
		return &ConfigError{Reason: "no SPIFFE trust domain"}
	}
	u, err := url.Parse("spiffe://" + td)
	if err != nil || u.Host != td || u.Port() != "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return &ConfigError{Reason: fmt.Sprintf("SPIFFE trust domain %q is not a bare host", td)}
	}
	return nil
}

// RefError reports a grant reference that grant kind never made.
type RefError struct {
	Kind serviceplane.GrantKind
	Ref  string
}

// Error returns a human-readable description.
func (e *RefError) Error() string {
	return fmt.Sprintf("grants: %s: malformed reference %q", e.Kind, e.Ref)
}

// ConfigError reports a grant built without what it needs.
type ConfigError struct {
	Kind   serviceplane.GrantKind
	Reason string
}

// Error returns a human-readable description.
func (e *ConfigError) Error() string {
	if e.Kind == "" {
		return "grants: " + e.Reason
	}
	return "grants: " + string(e.Kind) + ": " + e.Reason
}

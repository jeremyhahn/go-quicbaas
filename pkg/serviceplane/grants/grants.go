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
//     profile, spiffe://<tenant>/svc/<name>;
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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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
	Destroy(ctx context.Context, path string) error
}

// NameRegistry is the slice of pkg/registry the name grant uses.
// *registry.Registry satisfies it.
type NameRegistry interface {
	Register(ctx context.Context, inst registry.ServiceInstance, ttl time.Duration) (int64, error)
	Deregister(ctx context.Context, leaseID int64) error
}

// RoleBinder binds a built-in role to a principal within a tenant. It has
// the shape of the IAM role assignment in pkg/service.
type RoleBinder interface {
	AssignRole(ctx context.Context, tenantID, principal, role string) error
	RevokeRole(ctx context.Context, tenantID, principal, role string) error
}

// SPIFFEID is the workload identity of a service: the tenant is the trust
// domain.
func SPIFFEID(m *serviceplane.Manifest) string {
	return "spiffe://" + m.Tenant + "/svc/" + m.Name
}

// SecretPrefix is the service's secret namespace.
func SecretPrefix(m *serviceplane.Manifest) string {
	return "services/" + m.Name + "/"
}

// Paths of the credentials the grants keep for the launcher.
func svidPath(m *serviceplane.Manifest) string { return SecretPrefix(m) + "svid" }
func tlsPath(m *serviceplane.Manifest) string  { return SecretPrefix(m) + "tls" }
func bindingPath(m *serviceplane.Manifest) string {
	return SecretPrefix(m) + "binding"
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
	// Validity is the SVID lifetime; zero takes the profile's default.
	Validity time.Duration
}

// Kind answers identity.
func (*Identity) Kind() serviceplane.GrantKind { return serviceplane.GrantIdentity }

// Grant issues the SVID and keeps it with its key. Its reference is
// <spiffe id>#<hex serial>.
func (g *Identity) Grant(ctx context.Context, m *serviceplane.Manifest, prior string) (string, error) {
	id := SPIFFEID(m)
	if strings.HasPrefix(prior, id+"#") {
		return prior, nil
	}
	serial, err := issue(tenantCtx(ctx, m), g.CA, g.Secrets, svidPath(m), id, &xkmssdk.TrustedCASignOptions{
		Profile:  xkmssdk.ProfileNameX509SVID,
		Validity: g.Validity,
		SANS:     &xkmssdk.TrustedCASubjectAlternativeNames{URIs: []string{id}},
	})
	if err != nil {
		return "", fmt.Errorf("identity %s: %w", id, err)
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
	if prior != "" {
		return prior, nil
	}
	if g.Domain == "" {
		return "", &ConfigError{Kind: serviceplane.GrantCertificate, Reason: "no domain"}
	}
	dns := g.DNSName(m)
	serial, err := issue(tenantCtx(ctx, m), g.CA, g.Secrets, tlsPath(m), dns, &xkmssdk.TrustedCASignOptions{
		Profile: xkmssdk.ProfileNameACMELeaf,
		SANS:    &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{dns}},
	})
	if err != nil {
		return "", fmt.Errorf("certificate %s: %w", dns, err)
	}
	return serial, nil
}

// Revoke revokes the leaf and drops it.
func (g *Certificate) Revoke(ctx context.Context, m *serviceplane.Manifest, ref string) error {
	return withdraw(tenantCtx(ctx, m), g.CA, g.Secrets, tlsPath(m), serviceplane.GrantCertificate, ref)
}

// issue makes a P-256 key and a CSR, has the CA sign it and keeps the
// certificate and key at path. It answers the hex serial.
func issue(ctx context.Context, ca CA, store SecretStore, path, cn string, opts *xkmssdk.TrustedCASignOptions) (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}, key)
	if err != nil {
		return "", err
	}
	cert, err := ca.SignCSR(ctx, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}), opts)
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

// withdraw revokes a serial and destroys the kept credential.
func withdraw(ctx context.Context, ca CA, store SecretStore, path string, kind serviceplane.GrantKind, hexSerial string) error {
	serial, ok := new(big.Int).SetString(hexSerial, 16)
	if !ok {
		return &RefError{Kind: kind, Ref: hexSerial}
	}
	return errors.Join(ca.Revoke(ctx, serial, reasonCessation), store.Destroy(ctx, path))
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
}

// Kind answers name.
func (*Name) Kind() serviceplane.GrantKind { return serviceplane.GrantName }

// Grant registers the name at <name>.<tenant>.<domain>:<first port>. Its
// reference is the lease ID.
func (g *Name) Grant(ctx context.Context, m *serviceplane.Manifest, prior string) (string, error) {
	if prior != "" {
		return prior, nil
	}
	if g.Domain == "" {
		return "", &ConfigError{Kind: serviceplane.GrantName, Reason: "no domain"}
	}
	addr := m.Name + "." + m.Tenant + "." + g.Domain
	if len(m.Ports) > 0 {
		addr += ":" + strconv.Itoa(int(m.Ports[0].Port))
	}
	lease, err := g.Registry.Register(tenantCtx(ctx, m), registry.ServiceInstance{
		ID: m.Name, Service: m.Name, Address: addr,
		Metadata: map[string]string{"spiffe_id": SPIFFEID(m)},
	}, g.TTL)
	if err != nil {
		return "", fmt.Errorf("name %s: %w", m.Name, err)
	}
	return strconv.FormatInt(lease, 10), nil
}

// Revoke deregisters the name. A lease already expired is gone.
func (g *Name) Revoke(ctx context.Context, m *serviceplane.Manifest, ref string) error {
	lease, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return &RefError{Kind: serviceplane.GrantName, Ref: ref}
	}
	err = g.Registry.Deregister(tenantCtx(ctx, m), lease)
	var gone *registry.LeaseNotFoundError
	if errors.As(err, &gone) {
		return nil
	}
	return err
}

// ---------------------------------------------------------------------------
// rbac

// RBAC binds the manifest's built-in role to the workload identity.
type RBAC struct{ Roles RoleBinder }

// Kind answers rbac.
func (*RBAC) Kind() serviceplane.GrantKind { return serviceplane.GrantRBAC }

// Grant binds the role. Its reference is <role>@<spiffe id>. A redeploy
// that changes the role binds the new one and unbinds the prior.
func (g *RBAC) Grant(ctx context.Context, m *serviceplane.Manifest, prior string) (string, error) {
	id := SPIFFEID(m)
	ref := m.Role + "@" + id
	if prior == ref {
		return ref, nil
	}
	ctx = tenantCtx(ctx, m)
	if err := g.Roles.AssignRole(ctx, m.Tenant, id, m.Role); err != nil {
		return "", fmt.Errorf("rbac %s: %w", ref, err)
	}
	if prior != "" {
		if err := g.revoke(ctx, m, prior); err != nil {
			return "", errors.Join(err, g.Roles.RevokeRole(ctx, m.Tenant, id, m.Role))
		}
	}
	return ref, nil
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
type Secrets struct{ Secrets SecretStore }

// Kind answers secrets.
func (*Secrets) Kind() serviceplane.GrantKind { return serviceplane.GrantSecrets }

// Grant writes the binding. Its reference is the namespace.
func (g *Secrets) Grant(ctx context.Context, m *serviceplane.Manifest, prior string) (string, error) {
	if prior != "" {
		return prior, nil
	}
	id := SPIFFEID(m)
	err := g.Secrets.Store(tenantCtx(ctx, m), bindingPath(m),
		map[string][]byte{"spiffe_id": []byte(id)},
		map[string]string{"bound_to": id, "namespace": SecretPrefix(m)})
	if err != nil {
		return "", fmt.Errorf("secrets %s: %w", SecretPrefix(m), err)
	}
	return SecretPrefix(m), nil
}

// Revoke removes the binding.
func (g *Secrets) Revoke(ctx context.Context, m *serviceplane.Manifest, _ string) error {
	return g.Secrets.Destroy(tenantCtx(ctx, m), bindingPath(m))
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
	return []serviceplane.Grant{
		&Identity{CA: c.CA, Secrets: c.Secrets, Validity: c.SVIDValidity},
		&Certificate{CA: c.CA, Secrets: c.Secrets, Domain: c.Domain},
		&Name{Registry: c.Registry, TTL: c.NameTTL, Domain: c.Domain},
		&RBAC{Roles: c.Roles},
		&Secrets{Secrets: c.Secrets},
	}, nil
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

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

// Package certfixture issues the integration suite's TLS and PIV material
// through QuicBaaS's OWN certificate authority.
//
// # Why this package exists
//
// Until go-quicbaas#943 the integration cluster's certificates came from a
// 444-line parallel certificate authority (test/integration/gen-certs) that
// built its own roots and leaves with raw crypto/ecdsa and crypto/x509. The
// suite therefore proved the system worked against certificates NO PART OF THE
// PRODUCT ISSUES: every divergence between that lookalike and the real issuance
// path — extensions, SPIFFE URI shape, key usage, chain construction — was
// invisible to every test in the suite, and the repository carried a committed
// 5 MB binary build of it at its root.
//
// # What replaces it
//
// One CA, built by the same construction the unit suite uses
// (pkg/testutil/xkmsca), and one issuance path: pkg/server.NewCAService, the
// adapter the daemon serves every certificate operation through on all six
// transports. Inside it, go-xkms does all of the cryptography — the requester
// key comes from the single audited generator (pkg/xkms.GenerateRequesterKey),
// the CSR from go-xkms CreateCSRWithKey, the signature from the go-xkms CA's
// SignCSR, which is the stack's one x509.CreateCertificate call. Nothing in
// this package creates a key or signs a certificate, and
// `make check-local-keygen` now covers test/ so nothing here can start.
//
// # Why the CA is on disk
//
// Because the fixture set is not the whole story. The suite also mints
// per-tenant client certificates at runtime (helpers.AuthTenantAdmin) and
// software PIV credentials (the login suite), and those have to chain to the
// same trust anchor the cluster was started with. A CA that lived only in the
// generating process would force those callers back to signing things
// themselves — which is exactly the defect. So the CA's key and certificate
// stores are go-xkms file-backed stores under the fixture directory, and every
// later caller OPENS that CA and issues through the product path.
package certfixture

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"net"
	"path/filepath"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// Profile names the go-xkms certificate profile an issuance runs under. They
// are the built-in profile names; the empty profile is a TLS leaf carrying both
// server and client extended key usage.
const (
	// ProfileClient is TLS client authentication — the human-login profile.
	// pkg/service.SPIFFEKindForProfile maps it to the SPIFFE "user" kind, so a
	// login credential must be issued under it or it is stamped as a service.
	ProfileClient = "client"

	// ProfileDual is the empty profile: a TLS leaf valid for BOTH server and
	// client authentication, which is what a cluster node needs (it serves TLS
	// and dials its peers over mTLS with the same certificate).
	ProfileDual = ""
)

// keystoreSubdir is the directory, inside the fixture directory, holding the
// CA's go-xkms key and certificate stores.
//
// It is a subdirectory rather than the fixture directory itself so the PEM
// fixtures a container mounts stay a flat, obvious set of files, and so the
// keystore can be given different mount semantics from the certificates when a
// harness wants the nodes to see the trust anchor without the signing key.
const keystoreSubdir = "ca-keystore"

// Config describes the fixture CA.
type Config struct {
	// Dir is the fixture directory: PEM fixtures are written here and the CA's
	// key and certificate stores live in the ca-keystore subdirectory.
	// Required.
	Dir string

	// CommonName names the root. Empty means DefaultCACommonName.
	CommonName string

	// Organization is the subject organization of the root and of every leaf it
	// issues. Empty means DefaultOrganization.
	Organization string

	// SPIFFETrustDomain is the trust domain stamped into the tenant-scoped
	// SPIFFE URI SAN of every client certificate issued with a tenant. It MUST
	// equal iam.spiffe_trust_domain on the nodes: login pins the configured
	// value against the presented one, so a mismatch fails every login closed.
	SPIFFETrustDomain string
}

// Defaults applied to a Config's empty fields.
const (
	// DefaultCACommonName names the fixture root.
	DefaultCACommonName = "QuicBaaS Integration CA"

	// DefaultOrganization is the subject organization of the root and its
	// leaves.
	DefaultOrganization = "QuicBaaS Integration Tests"

	// DefaultValidityDays is the lifetime of an issued leaf when a fixture
	// names none.
	DefaultValidityDays = 365
)

// Authority is an open fixture CA that issues through the product's own CA
// adapter.
type Authority struct {
	ca           xkmssdk.TrustedCA
	svc          xkmstransport.CAService
	organization string
	trustDomain  string
}

// Open brings up the fixture CA under cfg.Dir, creating it on first call and
// loading the existing one on every call after that, and wraps it in the CA
// adapter the daemon serves certificate operations through.
//
// Open is idempotent and safe to call from a second process: the load-then-init
// order means a caller that arrives after the material exists adopts it instead
// of minting a second root under the same subject name.
func Open(ctx context.Context, cfg Config) (*Authority, error) {
	if strings.TrimSpace(cfg.Dir) == "" {
		return nil, ErrNoDir
	}

	commonName := cfg.CommonName
	if commonName == "" {
		commonName = DefaultCACommonName
	}
	organization := cfg.Organization
	if organization == "" {
		organization = DefaultOrganization
	}

	keystoreDir := KeystoreDir(cfg.Dir)
	ca, err := xkmsca.Open(ctx, xkmsca.Config{
		CommonName:       commonName,
		Organization:     organization,
		CertValidityDays: DefaultValidityDays,
		Dir:              keystoreDir,
	})
	if err != nil {
		return nil, &AuthorityError{Op: "open ca", Dir: keystoreDir, Cause: err}
	}

	// The adapter gets no trust domain: it would derive the SPIFFE name from
	// the common name, which for a login credential is an email that go-qrdb
	// refuses as a path segment. The fixture adds the SPIFFE SAN itself.
	svc, err := server.NewCAService(ca, "")
	if err != nil {
		return nil, &AuthorityError{Op: "ca service", Dir: keystoreDir, Cause: err}
	}

	return &Authority{
		ca:           ca,
		svc:          svc,
		organization: organization,
		trustDomain:  cfg.SPIFFETrustDomain,
	}, nil
}

// KeystoreDir returns the directory, inside a fixture directory, that holds the
// CA's key and certificate stores. Exported so a harness can mount or clean it
// without knowing the layout.
func KeystoreDir(dir string) string { return filepath.Join(dir, keystoreSubdir) }

// TrustDomain returns the SPIFFE trust domain this authority stamps.
func (a *Authority) TrustDomain() string { return a.trustDomain }

// Issued is one issued credential: the certificate, the chain, and the private
// key the REQUESTER holds.
//
// The key is present because the product's issuance path generates it outside
// the CA's keystore on purpose — a credential the requester holds is not
// platform key material, so the CA only ever sees the CSR. A keystore-generated
// key would come back as a non-exportable opaque handle and no fixture could
// use it. See pkg/xkms/keygen.go.
type Issued struct {
	// CertificatePEM is the issued leaf.
	CertificatePEM []byte

	// PrivateKeyPEM is the requester's private key.
	PrivateKeyPEM []byte

	// ChainPEM is the issuing CA's chain, or nil when the CA cannot produce
	// one.
	ChainPEM []byte

	// Certificate is the parsed leaf.
	Certificate *x509.Certificate

	// Signer is the parsed private key, for a caller that must sign with the
	// credential rather than just present it.
	Signer crypto.Signer
}

// ServerFixture describes a node's serving credential: a TLS leaf valid for
// both server and client authentication, under the names the node is reached
// by.
//
// Nothing here is defaulted from a constant. The hostnames and addresses a
// cluster serves under belong to the harness that defines the cluster, not to
// the code that signs certificates — baking them in is what made the old
// generator impossible to reuse and impossible to release.
type ServerFixture struct {
	// File is the fixture file stem: "node1" writes node1.crt and node1.key.
	File string `json:"file"`

	// CommonName is the subject common name.
	CommonName string `json:"common_name"`

	// DNSNames are the dNSName SANs.
	DNSNames []string `json:"dns_names,omitempty"`

	// IPAddresses are the iPAddress SANs, in textual form.
	IPAddresses []string `json:"ip_addresses,omitempty"`

	// ValidityDays is the leaf lifetime. Zero means DefaultValidityDays.
	ValidityDays int `json:"validity_days,omitempty"`
}

// ClientFixture describes a client credential: a TLS client-authentication leaf
// carrying an email SAN and, when a tenant is named, a tenant-scoped SPIFFE URI
// SAN that login reads the tenant from.
type ClientFixture struct {
	// File is the fixture file stem: "admin" writes admin.crt and admin.key.
	File string `json:"file"`

	// CommonName is the subject common name. For a login credential this is
	// the principal's email. The SPIFFE SAN's name segment is SPIFFEName of
	// it: "admin@integration.test" carries spiffe://<tenant>/user/admin.
	CommonName string `json:"common_name"`

	// Email, when set, is added as an rfc822Name SAN so a server-side identity
	// resolver can read it.
	Email string `json:"email,omitempty"`

	// TenantID scopes the credential to a tenant. Empty issues no SPIFFE SAN,
	// which makes the credential unusable for login — only do that for a
	// credential that is verified by chain alone.
	TenantID string `json:"tenant_id,omitempty"`

	// ValidityDays is the leaf lifetime. Zero means DefaultValidityDays.
	ValidityDays int `json:"validity_days,omitempty"`
}

// IssueServer issues a node's serving credential under the system tenant:
// go-xkms v0.58 refuses a request without a tenant, and names `system` the
// tenant of the node's own certificates.
func (a *Authority) IssueServer(ctx context.Context, f ServerFixture) (*Issued, error) {
	if f.CommonName == "" {
		return nil, ErrNoCommonName
	}
	sans := make([]string, 0, len(f.DNSNames)+len(f.IPAddresses)+1)
	for _, name := range f.DNSNames {
		sans = append(sans, "DNS:"+name)
	}
	for _, addr := range f.IPAddresses {
		if net.ParseIP(addr) == nil {
			return nil, &IssueError{CommonName: f.CommonName, Cause: &InvalidIPError{Address: addr}}
		}
		sans = append(sans, "IP:"+addr)
	}
	if a.trustDomain != "" {
		uri, err := spiffeURISAN(tenantctx.SystemTenantID, qrdbsdk.SpiffeKindService, f.CommonName)
		if err != nil {
			return nil, &IssueError{CommonName: f.CommonName, Cause: err}
		}
		sans = append(sans, uri)
	}
	return a.issue(ctx, &xkmstransport.IssueCertificateRequest{
		Profile:      ProfileDual,
		CommonName:   f.CommonName,
		Organization: a.organization,
		SANs:         sans,
		ValidityDays: validity(f.ValidityDays),
		TenantID:     tenantctx.SystemTenantID,
	})
}

// IssueClient issues a client credential. When f.TenantID is set the authority
// must have a trust domain, because the tenant-scoped SPIFFE SAN is what login
// resolves the tenant from.
func (a *Authority) IssueClient(ctx context.Context, f ClientFixture) (*Issued, error) {
	if f.CommonName == "" {
		return nil, ErrNoCommonName
	}
	if f.TenantID != "" && a.trustDomain == "" {
		return nil, ErrNoTrustDomain
	}
	var sans []string
	if f.Email != "" {
		sans = append(sans, "Email:"+f.Email)
	}
	if f.TenantID != "" {
		uri, err := spiffeURISAN(f.TenantID, qrdbsdk.SpiffeKindUser, f.CommonName)
		if err != nil {
			return nil, &IssueError{CommonName: f.CommonName, Cause: err}
		}
		sans = append(sans, uri)
	}
	return a.issue(ctx, &xkmstransport.IssueCertificateRequest{
		Profile:      ProfileClient,
		CommonName:   f.CommonName,
		Organization: a.organization,
		SANs:         sans,
		ValidityDays: validity(f.ValidityDays),
		TenantID:     f.TenantID,
	})
}

// issue runs one request through the product's CA adapter and decodes what it
// returns.
func (a *Authority) issue(ctx context.Context, req *xkmstransport.IssueCertificateRequest) (*Issued, error) {
	resp, err := a.svc.IssueCertificate(ctx, req)
	if err != nil {
		return nil, &IssueError{CommonName: req.CommonName, Cause: err}
	}
	if len(resp.PrivateKeyPEM) == 0 {
		return nil, &IssueError{CommonName: req.CommonName, Cause: ErrNoPrivateKey}
	}

	cert, err := decodeCertificate(resp.CertificatePEM)
	if err != nil {
		return nil, &IssueError{CommonName: req.CommonName, Cause: err}
	}
	signer, err := decodeSigner(resp.PrivateKeyPEM)
	if err != nil {
		return nil, &IssueError{CommonName: req.CommonName, Cause: err}
	}

	return &Issued{
		CertificatePEM: resp.CertificatePEM,
		PrivateKeyPEM:  resp.PrivateKeyPEM,
		ChainPEM:       resp.ChainPEM,
		Certificate:    cert,
		Signer:         signer,
	}, nil
}

// CACertificatePEM returns the authority's own certificate, the trust anchor
// every fixture chains to.
func (a *Authority) CACertificatePEM() ([]byte, error) {
	cert, err := a.ca.CACertificate()
	if err != nil {
		return nil, &AuthorityError{Op: "ca certificate", Cause: err}
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), nil
}

// InvalidIPError reports an IP SAN that is not an address. It is a typed error
// rather than a silent drop because a node whose certificate quietly lost an
// address SAN fails at TLS handshake time, far from the cause.
type InvalidIPError struct {
	// Address is the value that did not parse.
	Address string
}

func (e *InvalidIPError) Error() string {
	return "certfixture: not an IP address: " + e.Address
}

// decodeCertificate parses a single PEM certificate.
func decodeCertificate(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, &DecodeError{What: "certificate"}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, &DecodeError{What: "certificate", Cause: err}
	}
	return cert, nil
}

// decodeSigner parses the private key the CA adapter returned.
//
// The decoder is go-xkms's own (DecodePrivateKeyPEM), which is the exact
// counterpart of the EncodePrivateKeyPEM the adapter encoded with. That
// pairing matters: go-xkms labels an ECDSA key's PEM block "EC PRIVATE KEY"
// while the DER inside it is PKCS#8, so crypto/x509.ParseECPrivateKey — the
// obvious-looking choice, and what the old fixture helpers used — fails on it.
func decodeSigner(keyPEM []byte) (crypto.Signer, error) {
	key, err := xkmssdk.DecodePrivateKeyPEM(keyPEM, nil)
	if err != nil {
		return nil, &DecodeError{What: "private key", Cause: err}
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, &DecodeError{What: "private key", Cause: ErrNotASigner}
	}
	return signer, nil
}

// validity applies DefaultValidityDays to an unset lifetime.
func validity(days int) int {
	if days > 0 {
		return days
	}
	return DefaultValidityDays
}

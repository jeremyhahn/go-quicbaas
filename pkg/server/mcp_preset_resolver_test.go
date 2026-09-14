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

package server

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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Test doubles
// =============================================================================

// fakeAgentCertIssuer is a test double for service.AgentCertIssuer. It records
// every request it received and MINTS A REAL CERTIFICATE from it — subject, OU
// and tenant SPIFFE SAN included — so the resolver's tenant derivation, SPKI
// binding and enrollment run against genuine x509 material rather than a
// placeholder. The private key comes back in a real mlocked buffer so the
// Bytes()/Destroy() lifecycle is exercised.
type fakeAgentCertIssuer struct {
	mu   sync.Mutex
	reqs []*service.AgentCertificateRequest
	err  error

	// omitSPIFFE drops the tenant SPIFFE SAN even when the request asks for
	// one, reproducing a certificate this build's issuer would not mint.
	omitSPIFFE bool

	// spiffeTenantOverride, when set, binds the certificate to a DIFFERENT
	// tenant than the request named.
	spiffeTenantOverride string

	// omitParsedCert leaves IssuedAgentCertificate.ParsedCert nil.
	omitParsedCert bool

	// malformedSPIFFE emits a SPIFFE SAN whose tenant segment is empty, the
	// shape go-qrdb's verifier fails closed on.
	malformedSPIFFE bool
}

func (f *fakeAgentCertIssuer) IssueAgentCertificate(_ context.Context, req *service.AgentCertificateRequest) (*service.IssuedAgentCertificate, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	scriptedErr := f.err
	omitSPIFFE := f.omitSPIFFE
	override := f.spiffeTenantOverride
	omitParsed := f.omitParsedCert
	malformed := f.malformedSPIFFE
	f.mu.Unlock()

	if scriptedErr != nil {
		return nil, scriptedErr
	}

	spiffeTenant := req.TenantID
	if override != "" {
		spiffeTenant = override
	}
	trustDomain := req.SPIFFETrustDomain
	if omitSPIFFE {
		trustDomain = ""
	}
	if malformed {
		// An empty tenant segment: a well-formed URI carrying an
		// unusable identity, which the verifier must reject rather
		// than read as "no SAN".
		spiffeTenant = " "
	}

	certPEM, keyPEM, parsed, err := mintTestAgentCert(req, trustDomain, spiffeTenant)
	if err != nil {
		return nil, err
	}
	// NewBufferFromBytes wipes its source slice, so hand it a copy.
	buf, err := secure.NewBufferFromBytes(append([]byte(nil), keyPEM...))
	if err != nil {
		return nil, err
	}
	issued := &service.IssuedAgentCertificate{
		CertPEM:    certPEM,
		PrivateKey: buf,
		CAPEM:      []byte("-----BEGIN CERTIFICATE-----\nCA\n-----END CERTIFICATE-----\n"),
		ParsedCert: parsed,
	}
	if omitParsed {
		issued.ParsedCert = nil
	}
	return issued, nil
}

// request returns the last request the issuer saw, or nil when it saw none.
func (f *fakeAgentCertIssuer) request() *service.AgentCertificateRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) == 0 {
		return nil
	}
	return f.reqs[len(f.reqs)-1]
}

// commonNames returns the CommonName of every certificate the issuer minted.
func (f *fakeAgentCertIssuer) commonNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.reqs))
	for _, req := range f.reqs {
		out = append(out, req.Subject.CommonName)
	}
	return out
}

// setError scripts issuance to fail.
func (f *fakeAgentCertIssuer) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// mintTestAgentCert produces a self-signed client certificate shaped exactly
// like the one the agent cert issuer produces for a spawn: O=<tenant>,
// OU=agents, CN=<requested>, and — when a trust domain is supplied — the
// tenant-scoped SPIFFE URI SAN go-qrdb derives the identity's tenant from.
func mintTestAgentCert(
	req *service.AgentCertificateRequest,
	trustDomain, spiffeTenant string,
) (certPEM, keyPEM []byte, parsed *x509.Certificate, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         req.Subject.CommonName,
			Organization:       []string{req.Subject.Organization},
			OrganizationalUnit: []string{req.Subject.OrganizationalUnit},
		},
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(time.Duration(req.ValidityDays) * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		DNSNames:    append([]string{req.Hostname}, req.ExtraDNSNames...),
	}
	if trustDomain != "" && spiffeTenant != "" {
		uri, parseErr := url.Parse("spiffe://" + trustDomain + "/tenant/" + spiffeTenant +
			"/" + qrdbsdk.SpiffeKindService + "/" + req.Subject.CommonName)
		if parseErr != nil {
			return nil, nil, nil, parseErr
		}
		template.URIs = []*url.URL{uri}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}
	parsed, err = x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		parsed, nil
}

// newSessionIdentityStore returns a REAL iam.IdentityStore over the in-memory
// storage — the same production enrollment code the server runs — plus the
// storage so a test can read back exactly what was written.
func newSessionIdentityStore(t *testing.T) (*iam.IdentityStore, *iam.MemoryStorage) {
	t.Helper()
	memory := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  memory,
		GroupStorage: memory,
		RoleStorage:  memory,
	})
	return store, memory
}

// newTestResolver wires a resolver over the supplied issuer and a real identity
// store, returning the storage so a test can inspect the enrollment.
func newTestResolver(t *testing.T, issuer service.AgentCertIssuer, defaultTenant string) (*AgentMCPPresetResolver, *iam.MemoryStorage) {
	t.Helper()
	store, memory := newSessionIdentityStore(t)
	r, err := NewAgentMCPPresetResolver(AgentMCPPresetResolverConfig{
		Issuer:            issuer,
		Identities:        staticIdentityStore(store),
		EndpointURL:       "https://127.0.0.1:11443/mcp",
		Hostname:          "host-1",
		DefaultTenant:     defaultTenant,
		SPIFFETrustDomain: testTrustDomain,
		Logger:            makeNoopLogger(),
	})
	if err != nil {
		t.Fatalf("NewAgentMCPPresetResolver: %v", err)
	}
	return r, memory
}

// staticIdentityStore adapts an already-constructed store to the per-spawn
// resolver seam.
func staticIdentityStore(store AgentSessionIdentityStore) AgentSessionIdentityStoreResolver {
	return func(context.Context) (AgentSessionIdentityStore, error) { return store, nil }
}

// =============================================================================
// go-quicbaas#901 — the session certificate must carry authority
// =============================================================================

// TestAgentMCPPresetResolver_Resolve_EnrollsTheSessionPrincipal is the
// regression guard for go-quicbaas#901. Minting a certificate conferred nothing:
// go-qrdb resolves an mTLS principal's roles from the user record its Subject
// CommonName names, found none, and proceeded with empty roles, so a spawned
// agent completed its handshake and was then refused every MCP tool but the
// public ones.
//
// After Resolve, the principal EXISTS in the tenant the certificate
// authenticates into, is active, is restricted to mTLS, is bound to the
// certificate's SPKI thumbprint, and holds exactly the session grant.
func TestAgentMCPPresetResolver_Resolve_EnrollsTheSessionPrincipal(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, memory := newTestResolver(t, issuer, "system")

	endpoint, env, cleanup, err := r.Resolve(context.Background(), "ses-xyz", "tenant-7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cleanup == nil {
		t.Fatal("Resolve returned nil cleanup")
	}
	defer cleanup()

	if endpoint != "https://127.0.0.1:11443/mcp" {
		t.Errorf("url = %q, want the configured endpoint", endpoint)
	}

	// The certificate request carries the tenant in O=, the agent OU sentinel,
	// the per-session CN, the short validity, and the trust domain without which
	// none of the rest could be authorized.
	req := issuer.request()
	if req.Subject.Organization != "tenant-7" {
		t.Errorf("cert O= = %q, want tenant-7", req.Subject.Organization)
	}
	if req.Subject.OrganizationalUnit != service.DefaultAgentCertOU {
		t.Errorf("cert OU = %q, want %q", req.Subject.OrganizationalUnit, service.DefaultAgentCertOU)
	}
	if req.Subject.CommonName != "agent-ses-xyz" {
		t.Errorf("cert CN = %q, want agent-ses-xyz", req.Subject.CommonName)
	}
	if req.ValidityDays != ephemeralAgentCertValidityDays {
		t.Errorf("validity = %d, want %d", req.ValidityDays, ephemeralAgentCertValidityDays)
	}
	if req.SPIFFETrustDomain != testTrustDomain {
		t.Errorf("spiffe trust domain = %q, want %q", req.SPIFFETrustDomain, testTrustDomain)
	}

	// The principal the authenticator will look for.
	principal, err := memory.GetUserByUsername(context.Background(), "tenant-7", "agent-ses-xyz")
	if err != nil {
		t.Fatalf("the session principal was not enrolled: %v", err)
	}
	if !principal.CanAuthenticate() {
		t.Fatalf("an enrolled session must be able to authenticate, lifecycle = %q", principal.Lifecycle)
	}
	if len(principal.AuthMethods) != 1 || principal.AuthMethods[0] != iam.AuthMethodMTLS {
		t.Errorf("auth methods = %v, want exactly [%s]", principal.AuthMethods, iam.AuthMethodMTLS)
	}
	if principal.Email != "" {
		t.Errorf("email = %q, want none: a session holds no mailbox", principal.Email)
	}

	roleID := iam.AgentSessionRoleID("tenant-7")
	if len(principal.Roles) != 1 || principal.Roles[0] != roleID {
		t.Fatalf("roles = %v, want exactly [%s]", principal.Roles, roleID)
	}

	// EXACTLY the session grant — no more.
	role, err := memory.GetRole(context.Background(), "tenant-7", roleID)
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	assertSameStrings(t, role.Permissions, iam.AgentSessionGrant())

	// The certificate is bound by the stack's one canonical mTLS identifier.
	parsedCert := parseCertFile(t, env[mcpCertEnvVar])
	want := qrdbsdk.ComputeSPKIThumbprint(parsedCert)
	if len(principal.CertBindings) != 1 {
		t.Fatalf("cert bindings = %d, want exactly 1", len(principal.CertBindings))
	}
	if principal.CertBindings[0].Thumbprint != want {
		t.Errorf("bound thumbprint = %q, want %q", principal.CertBindings[0].Thumbprint, want)
	}
	if principal.CertBindings[0].Kind != qrdbsdk.CertBindingSPKIThumbprint {
		t.Errorf("binding kind = %v, want the canonical SPKI kind", principal.CertBindings[0].Kind)
	}
}

// TestAgentMCPPresetResolver_Resolve_AttributesTheSessionNotTheDaemon pins WHICH
// principal a call from a spawned session is attributed to. go-qrdb authenticates
// an mTLS request as the certificate's Subject, so the subject the session's
// certificate carries is the subject every authorization decision and every audit
// record names — and it must be the SESSION's, distinct from the daemon identity
// running on the same host and from any other session.
func TestAgentMCPPresetResolver_Resolve_AttributesTheSessionNotTheDaemon(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, memory := newTestResolver(t, issuer, "system")

	// The daemon identity on this host: the subject a session must NOT be
	// attributed to. Its CN is hostname-derived and stable across restarts
	// (pkg/server startEmbeddedAgent), where a session's is per-spawn.
	const daemonCN = "agent-embedded-host-1"

	first, _, cleanupFirst, err := r.Resolve(context.Background(), "ses-one", "tenant-7")
	if err != nil {
		t.Fatalf("Resolve(ses-one): %v", err)
	}
	defer cleanupFirst()
	second, _, cleanupSecond, err := r.Resolve(context.Background(), "ses-two", "tenant-7")
	if err != nil {
		t.Fatalf("Resolve(ses-two): %v", err)
	}
	defer cleanupSecond()
	if first != second {
		t.Fatalf("both sessions must reach the same endpoint, got %q and %q", first, second)
	}

	one, err := memory.GetUserByUsername(context.Background(), "tenant-7", "agent-ses-one")
	if err != nil {
		t.Fatalf("session one was not enrolled: %v", err)
	}
	two, err := memory.GetUserByUsername(context.Background(), "tenant-7", "agent-ses-two")
	if err != nil {
		t.Fatalf("session two was not enrolled: %v", err)
	}
	if one.Username == two.Username || one.ID == two.ID {
		t.Fatalf("two sessions share a principal: %q/%s and %q/%s",
			one.Username, one.ID, two.Username, two.ID)
	}
	if one.Username == daemonCN || two.Username == daemonCN {
		t.Fatalf("a session is attributed to the daemon identity %q", daemonCN)
	}
	if one.CertBindings[0].Thumbprint == two.CertBindings[0].Thumbprint {
		t.Error("two sessions are bound to the same certificate")
	}
	if !strings.HasPrefix(one.Username, "agent-ses-") {
		t.Errorf("session subject = %q, want the per-session CN", one.Username)
	}
}

// TestAgentMCPPresetResolver_Resolve_GrantsNothingBeyondTheSessionFloor is the
// negative half. A spawned agent must not be able to administer the platform that
// runs it, so the role it holds carries no wildcard and no verb outside the
// pinned floor — in particular none of the destructive, secret, key-material,
// identity or control-plane verbs.
func TestAgentMCPPresetResolver_Resolve_GrantsNothingBeyondTheSessionFloor(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, memory := newTestResolver(t, issuer, "system")

	_, _, cleanup, err := r.Resolve(context.Background(), "ses-1", "acme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()

	role, err := memory.GetRole(context.Background(), "acme", iam.AgentSessionRoleID("acme"))
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}

	granted := make(map[string]bool, len(role.Permissions))
	for _, permission := range role.Permissions {
		if strings.Contains(permission, "*") {
			t.Fatalf("the session grant must never contain a wildcard, got %q", permission)
		}
		granted[permission] = true
	}
	forbidden := []string{
		"kv:delete",
		"secrets:read", "secrets:write",
		"kms:read", "kms:sign", "kms:decrypt",
		"iam:users:create", "iam:roles:create",
		"barrier:unseal", "seal:unseal", "ceremony:init",
		"cluster:admin", "tenant:create",
		"orchestrator:agent_heartbeat", "orchestrator:hook",
		"agent:runtime:spawn", "agent_accounts:materialize",
	}
	for _, permission := range forbidden {
		if granted[permission] {
			t.Errorf("a spawned agent session must not hold %q", permission)
		}
	}
	if len(role.Permissions) != len(iam.AgentSessionGrant()) {
		t.Errorf("role holds %d permissions, want exactly the %d in the pinned floor: %v",
			len(role.Permissions), len(iam.AgentSessionGrant()), role.Permissions)
	}
}

// TestAgentMCPPresetResolver_Cleanup_RevokesThePrincipal proves the principal
// cannot act after the session closes: the record the authenticator resolves
// roles from is gone, so the certificate authenticates into empty roles and the
// default-deny MCP gate refuses it. The ephemeral material goes with it.
func TestAgentMCPPresetResolver_Cleanup_RevokesThePrincipal(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, memory := newTestResolver(t, issuer, "system")

	_, env, cleanup, err := r.Resolve(context.Background(), "ses-close", "tenant-7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := memory.GetUserByUsername(context.Background(), "tenant-7", "agent-ses-close"); err != nil {
		t.Fatalf("precondition: the session principal must exist before close: %v", err)
	}

	dir := dirOf(env[mcpKeyEnvVar])
	cleanup()

	if _, err := memory.GetUserByUsername(context.Background(), "tenant-7", "agent-ses-close"); err == nil {
		t.Fatal("the session principal still resolves after close: its certificate can still act")
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("cleanup did not remove dir %s (stat err: %v)", dir, statErr)
	}
}

// TestAgentMCPPresetResolver_Cleanup_IsIdempotent covers a cleanup that runs
// twice — the ACP backend runs it on Close, and runs it immediately when the
// session can no longer be found, so both can happen for one spawn.
func TestAgentMCPPresetResolver_Cleanup_IsIdempotent(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, memory := newTestResolver(t, issuer, "system")

	_, _, cleanup, err := r.Resolve(context.Background(), "ses-twice", "tenant-7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	cleanup()
	cleanup()

	if _, err := memory.GetUserByUsername(context.Background(), "tenant-7", "agent-ses-twice"); err == nil {
		t.Fatal("the session principal still resolves after a repeated close")
	}
}

// TestAgentMCPPresetResolver_Resolve_ConcurrentSpawnsDoNotRace spawns many
// sessions at once in ONE tenant. They all converge the same tenant-scoped role
// — the case EnrollAgentCertIdentity was made race-safe for — so each must end
// up with its own principal holding exactly the floor, and the role must exist
// exactly once. Run with -race.
func TestAgentMCPPresetResolver_Resolve_ConcurrentSpawnsDoNotRace(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, memory := newTestResolver(t, issuer, "system")

	const spawns = 16
	type outcome struct {
		sessionID string
		cleanup   func()
		err       error
	}
	results := make(chan outcome, spawns)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < spawns; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			id := fmt.Sprintf("ses-%02d", i)
			_, _, cleanup, err := r.Resolve(context.Background(), id, "tenant-race")
			results <- outcome{sessionID: id, cleanup: cleanup, err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	cleanups := make([]func(), 0, spawns)
	for res := range results {
		if res.err != nil {
			t.Fatalf("concurrent Resolve(%s): %v", res.sessionID, res.err)
		}
		cleanups = append(cleanups, res.cleanup)
		principal, err := memory.GetUserByUsername(context.Background(), "tenant-race", "agent-"+res.sessionID)
		if err != nil {
			t.Fatalf("session %s was not enrolled: %v", res.sessionID, err)
		}
		roleID := iam.AgentSessionRoleID("tenant-race")
		if len(principal.Roles) != 1 || principal.Roles[0] != roleID {
			t.Errorf("session %s roles = %v, want exactly [%s]", res.sessionID, principal.Roles, roleID)
		}
	}

	// Every spawn minted its own certificate, so no two sessions share a
	// principal.
	seen := make(map[string]bool, spawns)
	for _, cn := range issuer.commonNames() {
		if seen[cn] {
			t.Errorf("two spawns minted the same CommonName %q", cn)
		}
		seen[cn] = true
	}
	if len(seen) != spawns {
		t.Errorf("minted %d distinct certificates, want %d", len(seen), spawns)
	}

	// The shared role converged once, to the floor.
	role, err := memory.GetRole(context.Background(), "tenant-race", iam.AgentSessionRoleID("tenant-race"))
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	assertSameStrings(t, role.Permissions, iam.AgentSessionGrant())

	// Closing every session concurrently revokes every principal.
	var closeWG sync.WaitGroup
	for _, c := range cleanups {
		closeWG.Add(1)
		go func(c func()) {
			defer closeWG.Done()
			c()
		}(c)
	}
	closeWG.Wait()
	users, err := memory.ListUsers(context.Background(), "tenant-race", iam.ListOptions{Limit: spawns + 1})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("%d session principals survived close, want 0", len(users))
	}
}

// TestAgentMCPPresetResolver_Resolve_FilesAndModes pins the on-disk contract the
// spawned agent reads: three files inside a 0700 directory, the private key
// strictly 0600, and a key the certificate can actually be presented with.
func TestAgentMCPPresetResolver_Resolve_FilesAndModes(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, _ := newTestResolver(t, issuer, "system")

	_, env, cleanup, err := r.Resolve(context.Background(), "ses-files", "tenant-7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()

	certPath := env[mcpCertEnvVar]
	keyPath := env[mcpKeyEnvVar]
	caPath := env[mcpCAEnvVar]
	if certPath == "" || keyPath == "" || caPath == "" {
		t.Fatalf("env missing cert/key/ca paths: %+v", env)
	}
	assertFileMode(t, certPath, 0644)
	assertFileMode(t, keyPath, 0600)
	assertFileMode(t, caPath, 0644)
	assertFileMode(t, dirOf(keyPath), 0700)

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatal("the key file holds no PEM block")
	}
	if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
		t.Fatalf("the key file does not hold a usable private key: %v", err)
	}
}

// TestAgentMCPPresetResolver_DefaultTenant uses the configured default tenant
// when the spawn supplies none, and enrolls the principal there.
func TestAgentMCPPresetResolver_DefaultTenant(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, memory := newTestResolver(t, issuer, "system")

	_, _, cleanup, err := r.Resolve(context.Background(), "ses-1", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()

	if issuer.request().Subject.Organization != "system" {
		t.Errorf("O= = %q, want default tenant 'system'", issuer.request().Subject.Organization)
	}
	if _, err := memory.GetUserByUsername(context.Background(), "system", "agent-ses-1"); err != nil {
		t.Fatalf("the session principal must be enrolled in the default tenant: %v", err)
	}
}

// TestAgentMCPPresetResolver_HostnameFallbackCN uses the hostname-derived CN when
// the spawn carries no session identifier, and enrolls that subject.
func TestAgentMCPPresetResolver_HostnameFallbackCN(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, memory := newTestResolver(t, issuer, "system")

	_, _, cleanup, err := r.Resolve(context.Background(), "  ", "tenant-7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()

	if got := issuer.request().Subject.CommonName; got != "agent-host-1" {
		t.Fatalf("CN = %q, want the hostname-derived agent-host-1", got)
	}
	if _, err := memory.GetUserByUsername(context.Background(), "tenant-7", "agent-host-1"); err != nil {
		t.Fatalf("the hostname-derived principal was not enrolled: %v", err)
	}
}

// =============================================================================
// Failure paths — every one of them must leave nothing behind
// =============================================================================

// TestAgentMCPPresetResolver_IssueError surfaces an MCPPresetResolveError when
// the issuer fails, and enrolls nothing.
func TestAgentMCPPresetResolver_IssueError(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	sealed := errors.New("ca sealed")
	issuer.setError(sealed)
	r, memory := newTestResolver(t, issuer, "system")

	endpoint, env, cleanup, err := r.Resolve(context.Background(), "ses-1", "tenant-1")
	if err == nil {
		t.Fatal("Resolve: expected error, got nil")
	}
	var resolveErr *MCPPresetResolveError
	if !errors.As(err, &resolveErr) {
		t.Fatalf("error type = %T, want *MCPPresetResolveError", err)
	}
	if !errors.Is(err, sealed) {
		t.Error("MCPPresetResolveError does not unwrap to the issuer cause")
	}
	if endpoint != "" || env != nil || cleanup != nil {
		t.Errorf("on error want zero values, got url=%q env=%v cleanup!=nil=%v", endpoint, env, cleanup != nil)
	}
	if users, _ := memory.ListUsers(context.Background(), "tenant-1", iam.ListOptions{Limit: 10}); len(users) != 0 {
		t.Errorf("a failed issuance enrolled %d principals, want 0", len(users))
	}
}

// TestAgentMCPPresetResolver_NoTenant denies issuance when neither the spawn nor
// the resolver default supplies a tenant.
func TestAgentMCPPresetResolver_NoTenant(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, _ := newTestResolver(t, issuer, "")

	if _, _, _, err := r.Resolve(context.Background(), "ses-1", ""); !errors.Is(err, ErrMCPPresetResolverNoTenant) {
		t.Fatalf("error = %v, want ErrMCPPresetResolverNoTenant", err)
	}
	if issuer.request() != nil {
		t.Error("a tenant-less spawn must not reach the issuer")
	}
}

// TestAgentMCPPresetResolver_NoSPIFFETenant refuses a certificate carrying no
// tenant SPIFFE SAN. go-qrdb would authenticate it into a tenant named by its OU
// — "agents" — so an enrollment in the spawning tenant could never authorize it.
// Handing the session a credential that resolves to nothing is exactly the defect
// #901 recorded, so the spawn gets no credential instead.
func TestAgentMCPPresetResolver_NoSPIFFETenant(t *testing.T) {
	issuer := &fakeAgentCertIssuer{omitSPIFFE: true}
	r, memory := newTestResolver(t, issuer, "system")

	_, _, cleanup, err := r.Resolve(context.Background(), "ses-1", "tenant-7")
	if err == nil {
		t.Fatal("Resolve: expected error, got nil")
	}
	var noSAN *AgentSessionCertNoSPIFFETenantError
	if !errors.As(err, &noSAN) {
		t.Fatalf("error type = %T (%v), want *AgentSessionCertNoSPIFFETenantError", err, err)
	}
	if noSAN.CommonName != "agent-ses-1" {
		t.Errorf("reported CN = %q, want agent-ses-1", noSAN.CommonName)
	}
	if cleanup != nil {
		t.Error("on error want nil cleanup")
	}
	if users, _ := memory.ListUsers(context.Background(), "tenant-7", iam.ListOptions{Limit: 10}); len(users) != 0 {
		t.Errorf("an unbindable certificate enrolled %d principals, want 0", len(users))
	}
}

// TestAgentMCPPresetResolver_SPIFFETenantMismatch refuses a certificate bound to
// a different tenant than the spawn named. Enrolling into either tenant would be
// wrong, so neither happens.
func TestAgentMCPPresetResolver_SPIFFETenantMismatch(t *testing.T) {
	issuer := &fakeAgentCertIssuer{spiffeTenantOverride: "other-tenant"}
	r, memory := newTestResolver(t, issuer, "system")

	_, _, _, err := r.Resolve(context.Background(), "ses-1", "tenant-7")
	if err == nil {
		t.Fatal("Resolve: expected error, got nil")
	}
	var mismatch *AgentSessionCertTenantMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error type = %T (%v), want *AgentSessionCertTenantMismatchError", err, err)
	}
	if mismatch.Certificate != "other-tenant" || mismatch.Spawn != "tenant-7" {
		t.Errorf("mismatch = %+v, want certificate=other-tenant spawn=tenant-7", *mismatch)
	}
	for _, tenant := range []string{"tenant-7", "other-tenant"} {
		if users, _ := memory.ListUsers(context.Background(), tenant, iam.ListOptions{Limit: 10}); len(users) != 0 {
			t.Errorf("tenant %s holds %d principals after a mismatch, want 0", tenant, len(users))
		}
	}
}

// TestAgentMCPPresetResolver_NoParsedCert refuses an issuance that produced no
// parsed leaf. Every identifying value of the principal is derived FROM the
// certificate, so there is nothing to enroll without it.
func TestAgentMCPPresetResolver_NoParsedCert(t *testing.T) {
	issuer := &fakeAgentCertIssuer{omitParsedCert: true}
	r, _ := newTestResolver(t, issuer, "system")

	if _, _, _, err := r.Resolve(context.Background(), "ses-1", "tenant-7"); !errors.Is(err, ErrMCPPresetResolverNoParsedCert) {
		t.Fatalf("error = %v, want ErrMCPPresetResolverNoParsedCert", err)
	}
}

// TestAgentMCPPresetResolver_IdentityStoreUnavailable refuses the spawn BEFORE
// minting anything when the identity store is not up. A certificate issued for a
// session that cannot be enrolled would be one more credential with no authority.
func TestAgentMCPPresetResolver_IdentityStoreUnavailable(t *testing.T) {
	newResolver := func(t *testing.T, identities AgentSessionIdentityStoreResolver) (*AgentMCPPresetResolver, *fakeAgentCertIssuer) {
		t.Helper()
		issuer := &fakeAgentCertIssuer{}
		r, err := NewAgentMCPPresetResolver(AgentMCPPresetResolverConfig{
			Issuer:            issuer,
			Identities:        identities,
			EndpointURL:       "https://h:1/mcp",
			Hostname:          "host-1",
			DefaultTenant:     "system",
			SPIFFETrustDomain: testTrustDomain,
			Logger:            makeNoopLogger(),
		})
		if err != nil {
			t.Fatalf("NewAgentMCPPresetResolver: %v", err)
		}
		return r, issuer
	}

	t.Run("resolver reports an error", func(t *testing.T) {
		down := errors.New("iam provider not ready")
		r, issuer := newResolver(t, func(context.Context) (AgentSessionIdentityStore, error) {
			return nil, down
		})
		if _, _, _, err := r.Resolve(context.Background(), "ses-1", "tenant-7"); !errors.Is(err, down) {
			t.Fatalf("error = %v, want it to unwrap to the store error", err)
		}
		if issuer.request() != nil {
			t.Error("no certificate may be minted for a session that cannot be enrolled")
		}
	})

	t.Run("resolver reports no store", func(t *testing.T) {
		r, issuer := newResolver(t, func(context.Context) (AgentSessionIdentityStore, error) {
			return nil, nil
		})
		if _, _, _, err := r.Resolve(context.Background(), "ses-1", "tenant-7"); !errors.Is(err, ErrMCPPresetResolverNoIdentityStore) {
			t.Fatalf("error = %v, want ErrMCPPresetResolverNoIdentityStore", err)
		}
		if issuer.request() != nil {
			t.Error("no certificate may be minted for a session that cannot be enrolled")
		}
	})
}

// scriptedIdentityStore is an AgentSessionIdentityStore whose two verbs can be
// scripted to fail, and which records what it was asked to revoke. It stands in
// for the store being unreachable BETWEEN the enrollment and the session's close
// — the window in which a principal exists but the session does not.
type scriptedIdentityStore struct {
	mu sync.Mutex

	enrolled  *iam.User
	enrollErr error
	revokeErr error

	revokedTenant   string
	revokedUsername string
	revokeCalls     int
}

func (s *scriptedIdentityStore) EnrollAgentSessionIdentity(_ context.Context, tenantID string, cert *x509.Certificate) (*iam.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enrollErr != nil {
		return nil, s.enrollErr
	}
	s.enrolled = &iam.User{
		TenantID: tenantID,
		Username: cert.Subject.CommonName,
		Roles:    []string{iam.AgentSessionRoleID(tenantID)},
	}
	return s.enrolled, nil
}

func (s *scriptedIdentityStore) RevokeAgentSessionIdentity(_ context.Context, tenantID, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokeCalls++
	s.revokedTenant = tenantID
	s.revokedUsername = username
	return s.revokeErr
}

// revocation returns what the store was asked to revoke and how often.
func (s *scriptedIdentityStore) revocation() (tenant, username string, calls int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revokedTenant, s.revokedUsername, s.revokeCalls
}

// newScriptedResolver wires a resolver over a scripted store.
func newScriptedResolver(t *testing.T, store *scriptedIdentityStore) (*AgentMCPPresetResolver, *fakeAgentCertIssuer) {
	t.Helper()
	issuer := &fakeAgentCertIssuer{}
	r, err := NewAgentMCPPresetResolver(AgentMCPPresetResolverConfig{
		Issuer:            issuer,
		Identities:        staticIdentityStore(store),
		EndpointURL:       "https://127.0.0.1:11443/mcp",
		Hostname:          "host-1",
		DefaultTenant:     "system",
		SPIFFETrustDomain: testTrustDomain,
		Logger:            makeNoopLogger(),
	})
	if err != nil {
		t.Fatalf("NewAgentMCPPresetResolver: %v", err)
	}
	return r, issuer
}

// TestAgentMCPPresetResolver_EnrollmentFailureRefusesTheSpawn covers the store
// refusing the enrollment. The spawn gets no credential at all: a certificate
// delivered without a principal behind it is precisely the state #901 recorded,
// so it must not be the outcome of a FAILED enrollment either.
func TestAgentMCPPresetResolver_EnrollmentFailureRefusesTheSpawn(t *testing.T) {
	refused := errors.New("tenant iam is sealed")
	store := &scriptedIdentityStore{enrollErr: refused}
	r, _ := newScriptedResolver(t, store)

	endpoint, env, cleanup, err := r.Resolve(context.Background(), "ses-1", "tenant-7")
	if err == nil {
		t.Fatal("Resolve: expected error, got nil")
	}
	if !errors.Is(err, refused) {
		t.Fatalf("error = %v, want it to unwrap to the store refusal", err)
	}
	var resolveErr *MCPPresetResolveError
	if !errors.As(err, &resolveErr) {
		t.Fatalf("error type = %T, want *MCPPresetResolveError", err)
	}
	if resolveErr.Op != "enroll session identity" {
		t.Errorf("Op = %q, want the enrollment stage", resolveErr.Op)
	}
	if endpoint != "" || env != nil || cleanup != nil {
		t.Errorf("on error want zero values, got url=%q env=%v cleanup!=nil=%v", endpoint, env, cleanup != nil)
	}
	// Nothing was enrolled, so nothing may be revoked: a revocation here would
	// be a delete addressed at a principal this spawn never created.
	if _, _, calls := store.revocation(); calls != 0 {
		t.Errorf("revoke was called %d times after a failed enrollment, want 0", calls)
	}
}

// TestAgentMCPPresetResolver_CleanupSurvivesAFailedRevocation covers the store
// being unreachable at session close. The close must still complete and still
// remove the on-disk material: a Close that gave up would leave the private key on
// disk as well as the principal in the store, which is strictly worse than leaving
// only the principal. The certificate's one-day expiry is the backstop underneath.
func TestAgentMCPPresetResolver_CleanupSurvivesAFailedRevocation(t *testing.T) {
	store := &scriptedIdentityStore{revokeErr: errors.New("raft not ready")}
	r, _ := newScriptedResolver(t, store)

	_, env, cleanup, err := r.Resolve(context.Background(), "ses-flaky", "tenant-7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	dir := dirOf(env[mcpKeyEnvVar])

	cleanup()

	tenant, username, calls := store.revocation()
	if calls != 1 {
		t.Errorf("revoke called %d times, want 1", calls)
	}
	if tenant != "tenant-7" || username != "agent-ses-flaky" {
		t.Errorf("revoked %q in %q, want agent-ses-flaky in tenant-7", username, tenant)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("a failed revocation left the key material at %s (stat err: %v)", dir, statErr)
	}
}

// TestNewAgentMCPPresetResolver_Validation covers the constructor guards. Each
// missing dependency is a wiring defect that must surface here rather than as a
// spawned agent with no authority.
func TestNewAgentMCPPresetResolver_Validation(t *testing.T) {
	store, _ := newSessionIdentityStore(t)
	complete := func() AgentMCPPresetResolverConfig {
		return AgentMCPPresetResolverConfig{
			Issuer:            &fakeAgentCertIssuer{},
			Identities:        staticIdentityStore(store),
			EndpointURL:       "https://h:1/mcp",
			Hostname:          "host",
			DefaultTenant:     "system",
			SPIFFETrustDomain: testTrustDomain,
			Logger:            makeNoopLogger(),
		}
	}

	if _, err := NewAgentMCPPresetResolver(complete()); err != nil {
		t.Fatalf("complete config: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*AgentMCPPresetResolverConfig)
		wantErr error
	}{
		{
			name:    "nil issuer",
			mutate:  func(c *AgentMCPPresetResolverConfig) { c.Issuer = nil },
			wantErr: ErrMCPPresetResolverNilIssuer,
		},
		{
			name:    "nil identities",
			mutate:  func(c *AgentMCPPresetResolverConfig) { c.Identities = nil },
			wantErr: ErrMCPPresetResolverNilIdentities,
		},
		{
			name:    "empty endpoint",
			mutate:  func(c *AgentMCPPresetResolverConfig) { c.EndpointURL = "  " },
			wantErr: ErrMCPPresetResolverNoEndpoint,
		},
		{
			name:    "empty hostname",
			mutate:  func(c *AgentMCPPresetResolverConfig) { c.Hostname = "" },
			wantErr: ErrMCPPresetResolverNoHostname,
		},
		{
			name:    "no trust domain",
			mutate:  func(c *AgentMCPPresetResolverConfig) { c.SPIFFETrustDomain = " " },
			wantErr: ErrMCPPresetResolverNoTrustDomain,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := complete()
			tt.mutate(&cfg)
			if _, err := NewAgentMCPPresetResolver(cfg); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestAgentMCPPresetResolver_MalformedSPIFFETenant fails closed on a well-formed
// SPIFFE URI carrying an unusable identity, exactly as go-qrdb's own verifier
// does with it. Reading it as "no SAN" would be a fail-open: the resolver would
// report a fixable misconfiguration for a certificate that is actually
// unverifiable.
func TestAgentMCPPresetResolver_MalformedSPIFFETenant(t *testing.T) {
	issuer := &fakeAgentCertIssuer{malformedSPIFFE: true}
	r, memory := newTestResolver(t, issuer, "system")

	_, _, cleanup, err := r.Resolve(context.Background(), "ses-1", "tenant-7")
	if err == nil {
		t.Fatal("Resolve: expected error, got nil")
	}
	var resolveErr *MCPPresetResolveError
	if !errors.As(err, &resolveErr) {
		t.Fatalf("error type = %T (%v), want *MCPPresetResolveError", err, err)
	}
	if resolveErr.Op != "verify certificate tenant binding" {
		t.Errorf("Op = %q, want the tenant-binding stage", resolveErr.Op)
	}
	// NOT the missing-SAN error: a malformed SAN is a different condition.
	var noSAN *AgentSessionCertNoSPIFFETenantError
	if errors.As(err, &noSAN) {
		t.Error("a malformed SPIFFE SAN must not be reported as an absent one")
	}
	if cleanup != nil {
		t.Error("on error want nil cleanup")
	}
	if users, _ := memory.ListUsers(context.Background(), "tenant-7", iam.ListOptions{Limit: 10}); len(users) != 0 {
		t.Errorf("an unverifiable certificate enrolled %d principals, want 0", len(users))
	}
}

// TestAgentMCPPresetResolver_RevokesWhenTheMaterialCannotBeWritten covers the
// window between enrollment and delivery. The principal exists from the moment
// the enrollment commits, so a failure to write the material the agent needs must
// take it back down — otherwise a spawn that never started would leave a live
// identity nothing will ever close.
func TestAgentMCPPresetResolver_RevokesWhenTheMaterialCannotBeWritten(t *testing.T) {
	issuer := &fakeAgentCertIssuer{}
	r, memory := newTestResolver(t, issuer, "system")

	// os.MkdirTemp resolves its parent through os.TempDir, which honours
	// TMPDIR. Pointing it at a path that does not exist fails the directory
	// creation without touching anything on the host.
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "no-such-parent"))

	_, _, cleanup, err := r.Resolve(context.Background(), "ses-nodir", "tenant-7")
	if err == nil {
		t.Fatal("Resolve: expected error, got nil")
	}
	var resolveErr *MCPPresetResolveError
	if !errors.As(err, &resolveErr) {
		t.Fatalf("error type = %T (%v), want *MCPPresetResolveError", err, err)
	}
	if resolveErr.Op != "create ephemeral cert dir" {
		t.Errorf("Op = %q, want the dir-creation stage", resolveErr.Op)
	}
	if cleanup != nil {
		t.Error("on error want nil cleanup: there is nothing left to clean up")
	}
	if _, err := memory.GetUserByUsername(context.Background(), "tenant-7", "agent-ses-nodir"); err == nil {
		t.Fatal("a failed delivery left a live session principal behind")
	}
}

// TestAgentMCPPresetResolverErrors_Messages pins the diagnostics an operator
// reads. Each one has to name the thing that has to change: the tenants that
// disagreed, the subject whose certificate carries no binding, and the stage that
// failed with its cause.
func TestAgentMCPPresetResolverErrors_Messages(t *testing.T) {
	mismatch := (&AgentSessionCertTenantMismatchError{
		Certificate: "other-tenant",
		Spawn:       "tenant-7",
	}).Error()
	for _, want := range []string{"other-tenant", "tenant-7"} {
		if !strings.Contains(mismatch, want) {
			t.Errorf("mismatch message %q does not name %q", mismatch, want)
		}
	}

	noSAN := (&AgentSessionCertNoSPIFFETenantError{CommonName: "agent-ses-1"}).Error()
	for _, want := range []string{"agent-ses-1", "SPIFFE"} {
		if !strings.Contains(noSAN, want) {
			t.Errorf("no-SAN message %q does not name %q", noSAN, want)
		}
	}

	cause := errors.New("ca sealed")
	wrapped := &MCPPresetResolveError{Op: "issue ephemeral agent certificate", Cause: cause}
	if !strings.Contains(wrapped.Error(), "issue ephemeral agent certificate") {
		t.Errorf("wrapped message %q does not name the stage", wrapped.Error())
	}
	if !strings.Contains(wrapped.Error(), "ca sealed") {
		t.Errorf("wrapped message %q does not name the cause", wrapped.Error())
	}
	if !errors.Is(wrapped, cause) {
		t.Error("MCPPresetResolveError does not unwrap to its cause")
	}
}

// =============================================================================
// The on-disk material
// =============================================================================

// TestCreateAgentSessionDir_IsOwnerOnly pins the directory the credential lives
// in. A world-traversable parent would make the 0600 on the key itself
// decorative.
func TestCreateAgentSessionDir_IsOwnerOnly(t *testing.T) {
	dir, err := createAgentSessionDir()
	if err != nil {
		t.Fatalf("createAgentSessionDir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	assertFileMode(t, dir, 0700)
}

// TestCreateAgentSessionDir_ReportsAnUncreatableDir covers the failure. A
// resolver that swallowed it would hand the agent paths inside a directory that
// does not exist.
func TestCreateAgentSessionDir_ReportsAnUncreatableDir(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "no-such-parent"))

	dir, err := createAgentSessionDir()
	if err == nil {
		_ = os.RemoveAll(dir)
		t.Fatal("expected an error, got nil")
	}
	var resolveErr *MCPPresetResolveError
	if !errors.As(err, &resolveErr) {
		t.Fatalf("error type = %T (%v), want *MCPPresetResolveError", err, err)
	}
	if resolveErr.Op != "create ephemeral cert dir" {
		t.Errorf("Op = %q, want the dir-creation stage", resolveErr.Op)
	}
	if dir != "" {
		t.Errorf("dir = %q, want empty on failure", dir)
	}
}

// newIssuedForWriteTest returns issued material with a real mlocked key buffer,
// so a test exercises the same Bytes()/Destroy() lifecycle production does.
func newIssuedForWriteTest(t *testing.T) *service.IssuedAgentCertificate {
	t.Helper()
	buf, err := secure.NewBufferFromBytes([]byte("-----BEGIN PRIVATE KEY-----\nKEY\n-----END PRIVATE KEY-----\n"))
	if err != nil {
		t.Fatalf("NewBufferFromBytes: %v", err)
	}
	return &service.IssuedAgentCertificate{
		CertPEM:    []byte("-----BEGIN CERTIFICATE-----\nLEAF\n-----END CERTIFICATE-----\n"),
		PrivateKey: buf,
		CAPEM:      []byte("-----BEGIN CERTIFICATE-----\nCA\n-----END CERTIFICATE-----\n"),
	}
}

// TestWriteAgentSessionMaterial_WritesAllThreeFiles pins the success path: three
// files with the right modes and the right bytes, and the env map that points the
// agent at them.
func TestWriteAgentSessionMaterial_WritesAllThreeFiles(t *testing.T) {
	dir := t.TempDir()
	issued := newIssuedForWriteTest(t)
	wantCert := append([]byte(nil), issued.CertPEM...)
	wantCA := append([]byte(nil), issued.CAPEM...)

	material, err := writeAgentSessionMaterial(dir, issued)
	if err != nil {
		t.Fatalf("writeAgentSessionMaterial: %v", err)
	}
	if material.dir != dir {
		t.Errorf("dir = %q, want %q", material.dir, dir)
	}
	assertFileMode(t, material.certPath, 0644)
	assertFileMode(t, material.keyPath, 0600)
	assertFileMode(t, material.caPath, 0644)

	if got, _ := os.ReadFile(material.certPath); string(got) != string(wantCert) {
		t.Errorf("cert contents = %q", string(got))
	}
	if got, _ := os.ReadFile(material.caPath); string(got) != string(wantCA) {
		t.Errorf("ca contents = %q", string(got))
	}
	if got, _ := os.ReadFile(material.keyPath); !strings.Contains(string(got), "PRIVATE KEY") {
		t.Errorf("key contents = %q, want the issued private key", string(got))
	}

	env := material.env()
	if env[mcpCertEnvVar] != material.certPath ||
		env[mcpKeyEnvVar] != material.keyPath ||
		env[mcpCAEnvVar] != material.caPath {
		t.Errorf("env does not point at the material: %+v", env)
	}
}

// TestWriteAgentSessionMaterial_ReportsEachUnwritableFile covers every write
// failure separately, because each one leaves a different amount already on disk
// and each has to be reported as its own stage. A directory standing where a file
// belongs makes the write fail deterministically without changing any permission
// the test process depends on.
func TestWriteAgentSessionMaterial_ReportsEachUnwritableFile(t *testing.T) {
	tests := []struct {
		name    string
		blocked string
		wantOp  string
	}{
		{name: "certificate", blocked: "cert.pem", wantOp: "write agent certificate"},
		{name: "private key", blocked: "key.pem", wantOp: "write agent private key"},
		{name: "ca bundle", blocked: "ca.pem", wantOp: "write ca bundle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, tt.blocked), 0700); err != nil {
				t.Fatalf("block %s: %v", tt.blocked, err)
			}
			issued := newIssuedForWriteTest(t)

			material, err := writeAgentSessionMaterial(dir, issued)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if material != nil {
				t.Error("a failed write must return no material for the caller to read")
			}
			var resolveErr *MCPPresetResolveError
			if !errors.As(err, &resolveErr) {
				t.Fatalf("error type = %T (%v), want *MCPPresetResolveError", err, err)
			}
			if resolveErr.Op != tt.wantOp {
				t.Errorf("Op = %q, want %q", resolveErr.Op, tt.wantOp)
			}
		})
	}
}

// =============================================================================
// The grant is one set with two spellings; they must never drift
// =============================================================================

// TestAgentSessionGrant_MatchesTheOrchestrationDeclaration pins
// iam.AgentSessionGrant — the set actually written into a tenant's role —
// against orchestration.AgentMCPScopes, the declaration. pkg/iam cannot import
// pkg/orchestration, so the literals are restated there; this test is what keeps
// the restatement honest. It also fails if the set grows a wildcard, because a
// wildcard here would hand every spawned agent in every tenant the resource it
// names.
func TestAgentSessionGrant_MatchesTheOrchestrationDeclaration(t *testing.T) {
	assertSameStrings(t, iam.AgentSessionGrant(), orchestration.AgentMCPScopes())

	for _, permission := range iam.AgentSessionGrant() {
		if strings.TrimSpace(permission) == "" {
			t.Error("the session grant contains a blank permission")
		}
		if strings.Contains(permission, "*") {
			t.Errorf("the session grant must never contain a wildcard, got %q", permission)
		}
		if !orchestration.IsValidScopeFormat(permission) {
			t.Errorf("%q is not a well-formed resource:action permission", permission)
		}
	}
}

// TestAgentSessionGrant_IsDisjointFromTheDaemonGrant pins the separation the two
// enrollments exist to express: a session is not a daemon. A session must hold no
// control-plane verb and a daemon no MCP floor verb — sharing either would make
// one enrollment a way to obtain the other's authority.
func TestAgentSessionGrant_IsDisjointFromTheDaemonGrant(t *testing.T) {
	session := make(map[string]bool)
	for _, permission := range iam.AgentSessionGrant() {
		session[permission] = true
	}
	for _, permission := range iam.AgentRuntimeGrant() {
		if session[permission] {
			t.Errorf("%q is held by BOTH an agent session and an agent daemon", permission)
		}
	}
	if iam.AgentSessionRoleName == iam.AgentRuntimeRoleName {
		t.Error("a session and a daemon must not converge the same role")
	}
	if iam.AgentSessionRoleID("t") == iam.AgentRuntimeRoleID("t") {
		t.Error("a session and a daemon must not share a role ID")
	}
}

// =============================================================================
// Helpers
// =============================================================================

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != want {
		t.Errorf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
	}
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}

// parseCertFile reads and parses the leaf certificate the resolver wrote.
func parseCertFile(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatalf("%s holds no PEM block", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cert
}

// assertSameStrings compares two string sets ignoring order.
func assertSameStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("set size = %d %v, want %d %v", len(got), got, len(want), want)
	}
	seen := make(map[string]int, len(got))
	for _, v := range got {
		seen[v]++
	}
	for _, v := range want {
		if seen[v] == 0 {
			t.Fatalf("missing %q; got %v, want %v", v, got, want)
		}
		seen[v]--
	}
}

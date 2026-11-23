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

package tenantagent_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantagent"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fakeKeys is an in-process KeyOps holding one P-256 key per key ID. It
// records every key ID it is asked to sign or verify with.
type fakeKeys struct {
	mu       sync.Mutex
	keys     map[string]*ecdsa.PrivateKey
	signed   []string
	verified []string
}

func newFakeKeys(t *testing.T, keyIDs ...string) *fakeKeys {
	t.Helper()
	f := &fakeKeys{keys: map[string]*ecdsa.PrivateKey{}}
	for _, id := range keyIDs {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		f.keys[id] = k
	}
	return f
}

func (f *fakeKeys) Sign(_ context.Context, keyID string, input []byte, hashAlgorithm string, preHashed bool) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signed = append(f.signed, keyID)
	k, ok := f.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("unknown key %q", keyID)
	}
	if hashAlgorithm != tenantagent.HashAlgorithm || preHashed {
		return nil, fmt.Errorf("unexpected hash %q prehashed=%v", hashAlgorithm, preHashed)
	}
	digest := sha256.Sum256(input)
	return ecdsa.SignASN1(rand.Reader, k, digest[:])
}

func (f *fakeKeys) Verify(_ context.Context, keyID string, input, signature []byte, hashAlgorithm string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verified = append(f.verified, keyID)
	k, ok := f.keys[keyID]
	if !ok {
		return false, fmt.Errorf("unknown key %q", keyID)
	}
	if hashAlgorithm != tenantagent.HashAlgorithm {
		return false, fmt.Errorf("unexpected hash %q", hashAlgorithm)
	}
	digest := sha256.Sum256(input)
	return ecdsa.VerifyASN1(&k.PublicKey, digest[:], signature), nil
}

func (f *fakeKeys) seen() (signed, verified []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.signed), slices.Clone(f.verified)
}

// newCert returns a self-signed certificate naming commonName.
func newCert(t *testing.T, commonName string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

type fixture struct {
	svc      *tenantagent.Service
	identity *iam.IdentityStore
	memory   *iam.MemoryStorage
	catalog  orchestration.AgentCatalogStore
	store    orchestration.TenantAgentStore
	keys     *fakeKeys
}

// newFixture builds a Service over real in-memory stores, with lead-1 and
// coder-1 in the catalog of every tenant named.
func newFixture(t *testing.T, tenants ...string) *fixture {
	t.Helper()
	memory := iam.NewMemoryStorage()
	f := &fixture{
		identity: iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: memory, RoleStorage: memory}),
		memory:   memory,
		catalog:  orchestration.NewInMemoryAgentCatalogStore(),
		store:    orchestration.NewInMemoryTenantAgentStore(),
	}
	keyIDs := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		keyIDs = append(keyIDs, tenantagent.SigningKeyFor(tenant))
		addAgents(t, f.catalog, tenant)
	}
	f.keys = newFakeKeys(t, keyIDs...)
	f.svc = f.newService(t, f.store)
	return f
}

func (f *fixture) newService(t *testing.T, store orchestration.TenantAgentStore) *tenantagent.Service {
	t.Helper()
	svc, err := tenantagent.New(tenantagent.Options{Catalog: f.catalog, Store: store, Identity: f.identity, Keys: f.keys})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return svc
}

func addAgents(t *testing.T, catalog orchestration.AgentCatalogStore, tenant string) {
	t.Helper()
	ctx := context.Background()
	lead := &orchestration.Agent{ID: "lead-1", Name: "Lead", Roles: []orchestration.AgentRole{orchestration.AgentRoleLead}, Backend: "codex", Model: "gpt-5.5"}
	coder := &orchestration.Agent{ID: "coder-1", Name: "Coder", Roles: []orchestration.AgentRole{orchestration.AgentRoleCoding}, Backend: "codex", Model: "gpt-5.5"}
	for _, agent := range []*orchestration.Agent{lead, coder} {
		if err := catalog.CreateAgent(ctx, tenant, agent); err != nil {
			t.Fatalf("CreateAgent(%s, %s) error = %v", tenant, agent.ID, err)
		}
	}
}

func addPool(t *testing.T, catalog orchestration.AgentCatalogStore, tenant, id string, agents ...string) {
	t.Helper()
	if err := catalog.CreatePool(context.Background(), tenant, &orchestration.AgentPool{ID: id, Name: id, Agents: agents}); err != nil {
		t.Fatalf("CreatePool(%s) error = %v", id, err)
	}
}

// userExists reports whether tenant has an iam user named username.
func (f *fixture) userExists(t *testing.T, tenant, username string) bool {
	t.Helper()
	user, err := f.memory.GetUserByUsername(context.Background(), tenant, username)
	return err == nil && user != nil
}

func TestAppoint_TheFirstTenantAgentOfAutomateTheThings(t *testing.T) {
	t.Parallel()

	const tenant = "automatethethings"
	f := newFixture(t, tenant)
	ctx := context.Background()

	record, err := f.svc.Appoint(ctx, tenant, tenantagent.AppointRequest{
		Agent:       orchestration.AgentRefTo("lead-1"),
		Certificate: newCert(t, "tenant-agent-automatethethings"),
	})
	if err != nil {
		t.Fatalf("Appoint() error = %v", err)
	}
	if record.TenantID != tenant || record.Principal != "tenant-agent-automatethethings" ||
		record.SigningKeyID != "tenant-agent-signing-automatethethings" || record.Agent != orchestration.AgentRefTo("lead-1") {
		t.Fatalf("record = %+v", record)
	}

	user, err := f.memory.GetUserByUsername(ctx, tenant, "tenant-agent-automatethethings")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if want := []string{iam.TenantAgentRoleID(tenant)}; !slices.Equal(user.Roles, want) {
		t.Fatalf("user roles = %v, want %v", user.Roles, want)
	}

	payload := []byte("the first Tenant Agent signs")
	keyID, sig, err := f.svc.Sign(ctx, tenant, payload)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if keyID != "tenant-agent-signing-automatethethings" {
		t.Fatalf("Sign() key = %q", keyID)
	}
	if err := f.svc.Verify(ctx, tenant, keyID, payload, sig); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if err := f.svc.Verify(ctx, tenant, keyID, []byte("tampered"), sig); !errors.Is(err, tenantagent.ErrBadSignature) {
		t.Fatalf("Verify(tampered) error = %v, want ErrBadSignature", err)
	}
	signed, verified := f.keys.seen()
	for _, id := range append(signed, verified...) {
		if id != keyID {
			t.Fatalf("key ops saw key %q, want only %q", id, keyID)
		}
	}
	if len(signed) != 1 || len(verified) != 2 {
		t.Fatalf("key ops signed %v and verified %v", signed, verified)
	}
}

func TestAppoint_RequiresALeadCapableAgent(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-a"
	tests := []struct {
		name    string
		ref     orchestration.AgentRef
		pool    []string
		wantErr error
	}{
		{name: "coding agent", ref: orchestration.AgentRefTo("coder-1"), wantErr: tenantagent.ErrNotLeadCapable},
		{name: "pool with a coding member", ref: orchestration.AgentRef{PoolID: "mixed"}, pool: []string{"lead-1", "coder-1"}, wantErr: tenantagent.ErrNotLeadCapable},
		{name: "unknown agent", ref: orchestration.AgentRefTo("ghost"), wantErr: orchestration.ErrAgentUnknown},
		{name: "unknown pool", ref: orchestration.AgentRef{PoolID: "ghosts"}, wantErr: orchestration.ErrAgentPoolUnknown},
		{name: "pool of leads", ref: orchestration.AgentRef{PoolID: "leads"}, pool: []string{"lead-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, tenant)
			if tt.pool != nil {
				addPool(t, f.catalog, tenant, tt.ref.PoolID, tt.pool...)
			}
			record, err := f.svc.Appoint(context.Background(), tenant, tenantagent.AppointRequest{
				Agent:       tt.ref,
				Certificate: newCert(t, "tenant-agent-a"),
			})
			if tt.wantErr == nil {
				if err != nil || record.Agent != tt.ref {
					t.Fatalf("Appoint() = %+v, %v", record, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Appoint() error = %v, want %v", err, tt.wantErr)
			}
			if f.userExists(t, tenant, "tenant-agent-a") {
				t.Fatal("a refused appointment enrolled a principal")
			}
			if _, err := f.store.GetTenantAgent(context.Background(), tenant); !errors.Is(err, orchestration.ErrTenantAgentUnknown) {
				t.Fatalf("a refused appointment stored a record: %v", err)
			}
		})
	}
}

func TestAppoint_OnePerTenant(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-a"
	f := newFixture(t, tenant)
	ctx := context.Background()

	first, err := f.svc.Appoint(ctx, tenant, tenantagent.AppointRequest{Agent: orchestration.AgentRefTo("lead-1"), Certificate: newCert(t, "tenant-agent-a")})
	if err != nil {
		t.Fatalf("Appoint() error = %v", err)
	}

	_, err = f.svc.Appoint(ctx, tenant, tenantagent.AppointRequest{Agent: orchestration.AgentRefTo("lead-1"), Certificate: newCert(t, "tenant-agent-usurper")})
	if !errors.Is(err, orchestration.ErrTenantAgentExists) {
		t.Fatalf("second Appoint() error = %v, want ErrTenantAgentExists", err)
	}
	if f.userExists(t, tenant, "tenant-agent-usurper") {
		t.Fatal("the refused second appointment enrolled its principal")
	}
	held, err := f.svc.Get(ctx, tenant)
	if err != nil || *held != *first {
		t.Fatalf("record after refusal = %+v, %v; want %+v", held, err, first)
	}

	addPool(t, f.catalog, tenant, "leads", "lead-1")
	again, err := f.svc.Appoint(ctx, tenant, tenantagent.AppointRequest{Agent: orchestration.AgentRef{PoolID: "leads"}, Certificate: newCert(t, "tenant-agent-a")})
	if err != nil {
		t.Fatalf("re-Appoint() error = %v", err)
	}
	if again.Agent.PoolID != "leads" || again.Principal != "tenant-agent-a" || !again.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("re-appointment = %+v, first = %+v", again, first)
	}
	if !f.userExists(t, tenant, "tenant-agent-a") {
		t.Fatal("re-appointment lost the principal")
	}
}

func TestAppoint_RefusesBadInput(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-a"
	f := newFixture(t, tenant)
	ctx := context.Background()
	lead := orchestration.AgentRefTo("lead-1")

	if _, err := f.svc.Appoint(ctx, " ", tenantagent.AppointRequest{Agent: lead, Certificate: newCert(t, "tenant-agent-a")}); !errors.Is(err, tenantagent.ErrTenantRequired) {
		t.Fatalf("empty tenant error = %v, want ErrTenantRequired", err)
	}

	var invalid *orchestration.InputValidationError
	if _, err := f.svc.Appoint(ctx, tenant, tenantagent.AppointRequest{Certificate: newCert(t, "tenant-agent-a")}); !errors.As(err, &invalid) {
		t.Fatalf("zero agent ref error = %v, want *InputValidationError", err)
	}
	if _, err := f.svc.Appoint(ctx, tenant, tenantagent.AppointRequest{Agent: lead}); !errors.As(err, &invalid) || invalid.Field != "certificate" {
		t.Fatalf("nil certificate error = %v, want *InputValidationError on certificate", err)
	}

	session := newCert(t, "tenant-agent-session")
	if _, err := f.identity.EnrollAgentSessionIdentity(ctx, tenant, session); err != nil {
		t.Fatalf("EnrollAgentSessionIdentity() error = %v", err)
	}
	var notTA *iam.NotATenantAgentIdentityError
	if _, err := f.svc.Appoint(ctx, tenant, tenantagent.AppointRequest{Agent: lead, Certificate: session}); !errors.As(err, &notTA) {
		t.Fatalf("session principal error = %v, want *iam.NotATenantAgentIdentityError", err)
	}

	if _, err := f.store.GetTenantAgent(ctx, tenant); !errors.Is(err, orchestration.ErrTenantAgentUnknown) {
		t.Fatalf("refused appointments stored a record: %v", err)
	}
	if f.userExists(t, tenant, "tenant-agent-a") {
		t.Fatal("refused appointments enrolled a principal")
	}
}

func TestTenantsAreIsolated(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tenant-a", "tenant-b")
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if _, err := f.svc.Appoint(ctx, tenant, tenantagent.AppointRequest{
			Agent:       orchestration.AgentRefTo("lead-1"),
			Certificate: newCert(t, "tenant-agent-"+tenant),
		}); err != nil {
			t.Fatalf("Appoint(%s) error = %v", tenant, err)
		}
	}
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		got, err := f.svc.Get(ctx, tenant)
		if err != nil || got.TenantID != tenant || got.Principal != "tenant-agent-"+tenant || got.SigningKeyID != tenantagent.SigningKeyFor(tenant) {
			t.Fatalf("Get(%s) = %+v, %v", tenant, got, err)
		}
	}

	payload := []byte("payload")
	_, sigA, err := f.svc.Sign(ctx, "tenant-a", payload)
	if err != nil {
		t.Fatalf("Sign(tenant-a) error = %v", err)
	}

	scoped := map[string]context.Context{
		"orchestration": orchestration.ContextWithTenant(ctx, "tenant-b"),
		"tenantctx":     tenantctx.WithTenant(ctx, "tenant-b"),
	}
	for name, bctx := range scoped {
		_, appointErr := f.svc.Appoint(bctx, "tenant-a", tenantagent.AppointRequest{Agent: orchestration.AgentRefTo("lead-1"), Certificate: newCert(t, "tenant-agent-a")})
		_, getErr := f.svc.Get(bctx, "tenant-a")
		_, _, signErr := f.svc.Sign(bctx, "tenant-a", payload)
		checks := map[string]error{
			"appoint": appointErr,
			"get":     getErr,
			"retire":  f.svc.Retire(bctx, "tenant-a"),
			"sign":    signErr,
			"verify":  f.svc.Verify(bctx, "tenant-a", tenantagent.SigningKeyFor("tenant-a"), payload, sigA),
		}
		for op, err := range checks {
			if !errors.Is(err, tenantagent.ErrWrongTenant) {
				t.Errorf("%s via %s context error = %v, want ErrWrongTenant", op, name, err)
			}
		}
	}
	if got, err := f.svc.Get(ctx, "tenant-a"); err != nil || got.Principal != "tenant-agent-tenant-a" {
		t.Fatalf("tenant-a after foreign calls = %+v, %v", got, err)
	}

	_, verifiedBefore := f.keys.seen()
	if err := f.svc.Verify(ctx, "tenant-a", tenantagent.SigningKeyFor("tenant-b"), payload, sigA); !errors.Is(err, tenantagent.ErrForeignKey) {
		t.Fatalf("Verify with tenant-b's key error = %v, want ErrForeignKey", err)
	}
	if _, verifiedAfter := f.keys.seen(); len(verifiedAfter) != len(verifiedBefore) {
		t.Fatalf("a foreign key reached the key ops: %v", verifiedAfter)
	}

	_, sigB, err := f.svc.Sign(ctx, "tenant-b", payload)
	if err != nil {
		t.Fatalf("Sign(tenant-b) error = %v", err)
	}
	if err := f.svc.Verify(ctx, "tenant-a", tenantagent.SigningKeyFor("tenant-a"), payload, sigB); !errors.Is(err, tenantagent.ErrBadSignature) {
		t.Fatalf("tenant-b's signature under tenant-a's key error = %v, want ErrBadSignature", err)
	}

	only := newFixture(t)
	addAgents(t, only.catalog, "tenant-b")
	if _, err := only.svc.Appoint(ctx, "tenant-a", tenantagent.AppointRequest{
		Agent:       orchestration.AgentRefTo("lead-1"),
		Certificate: newCert(t, "tenant-agent-a"),
	}); !errors.Is(err, orchestration.ErrAgentUnknown) {
		t.Fatalf("Appoint of tenant-b's agent in tenant-a error = %v, want ErrAgentUnknown", err)
	}
}

func TestRetire(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-a"
	f := newFixture(t, tenant)
	ctx := context.Background()
	if _, err := f.svc.Appoint(ctx, tenant, tenantagent.AppointRequest{Agent: orchestration.AgentRefTo("lead-1"), Certificate: newCert(t, "tenant-agent-a")}); err != nil {
		t.Fatalf("Appoint() error = %v", err)
	}

	if err := f.svc.Retire(ctx, tenant); err != nil {
		t.Fatalf("Retire() error = %v", err)
	}
	if _, err := f.svc.Get(ctx, tenant); !errors.Is(err, orchestration.ErrTenantAgentUnknown) {
		t.Fatalf("Get() after Retire error = %v, want ErrTenantAgentUnknown", err)
	}
	if f.userExists(t, tenant, "tenant-agent-a") {
		t.Fatal("Retire left the principal")
	}
	if err := f.svc.Retire(ctx, tenant); err != nil {
		t.Fatalf("second Retire() error = %v, want nil", err)
	}
	if _, _, err := f.svc.Sign(ctx, tenant, []byte("x")); !errors.Is(err, tenantagent.ErrNotAppointed) {
		t.Fatalf("Sign() after Retire error = %v, want ErrNotAppointed", err)
	}
}

// failingCreateStore is a TenantAgentStore whose every create fails.
type failingCreateStore struct {
	orchestration.TenantAgentStore
}

var errCreateFailed = errors.New("create failed")

func (failingCreateStore) CreateTenantAgent(context.Context, string, *orchestration.TenantAgent) error {
	return errCreateFailed
}

func TestAppoint_RevokesThePrincipalWhenTheWriteFails(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-a"
	f := newFixture(t, tenant)
	svc := f.newService(t, failingCreateStore{TenantAgentStore: f.store})

	_, err := svc.Appoint(context.Background(), tenant, tenantagent.AppointRequest{Agent: orchestration.AgentRefTo("lead-1"), Certificate: newCert(t, "tenant-agent-a")})
	if !errors.Is(err, errCreateFailed) {
		t.Fatalf("Appoint() error = %v, want the create error", err)
	}
	if f.userExists(t, tenant, "tenant-agent-a") {
		t.Fatal("a failed appointment left its principal enrolled")
	}
}

func TestNew_RequiresEveryDependency(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	full := tenantagent.Options{Catalog: f.catalog, Store: f.store, Identity: f.identity, Keys: f.keys}
	tests := map[string]func(o *tenantagent.Options){
		"catalog":  func(o *tenantagent.Options) { o.Catalog = nil },
		"store":    func(o *tenantagent.Options) { o.Store = nil },
		"identity": func(o *tenantagent.Options) { o.Identity = nil },
		"keys":     func(o *tenantagent.Options) { o.Keys = nil },
	}
	for name, drop := range tests {
		opts := full
		drop(&opts)
		if svc, err := tenantagent.New(opts); !errors.Is(err, tenantagent.ErrNotConfigured) || svc != nil {
			t.Errorf("New() without %s = %v, %v; want ErrNotConfigured", name, svc, err)
		}
	}
	if _, err := tenantagent.New(full); err != nil {
		t.Fatalf("New() with every dependency error = %v", err)
	}
}

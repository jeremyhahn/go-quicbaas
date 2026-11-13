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

//go:build integration

package helpers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

// PrincipalAuthError reports which step of provisioning or logging in a
// suite principal failed.
type PrincipalAuthError struct {
	// Principal names the principal, e.g. "data-plane" or "tenant".
	Principal string

	// Step names the failed step, e.g. "create role".
	Step string

	// TenantID is the principal's tenant, empty before it is created.
	TenantID string

	// Cause is the underlying failure.
	Cause error
}

func (e *PrincipalAuthError) Error() string {
	if e.TenantID == "" {
		return e.Principal + " auth: " + e.Step + ": " + e.Cause.Error()
	}
	return e.Principal + " auth: " + e.Step + " in tenant " + e.TenantID + ": " + e.Cause.Error()
}

func (e *PrincipalAuthError) Unwrap() error { return e.Cause }

// enrolledPrincipal is a suite principal enrolled in one cluster: its tenant,
// the client cert it logs in with and its bearer at each node.
type enrolledPrincipal struct {
	tenantID string
	certPEM  []byte
	keyPEM   []byte
	// tokens holds the principal's bearer per REST endpoint it logged in at:
	// each node signs with its own key, so a bearer is good only at the node
	// that issued it.
	tokens map[string]pivCachedToken
}

// principalEnroller provisions a principal in the cluster at baseURL. A
// failure is a *PrincipalAuthError.
type principalEnroller func(ctx context.Context, baseURL string) (*enrolledPrincipal, error)

// certLogin logs in with a client cert by mTLS and returns the bearer and its
// expires_in seconds.
type certLogin func(ctx context.Context, baseURL string, certPEM, keyPEM []byte) (bearer string, expiresIn int, err error)

// tenantDeleter deletes one tenant of the cluster at baseURL.
type tenantDeleter func(ctx context.Context, baseURL, tenantID string) error

// principalRegistry memoises one principal per cluster base URL, so a suite
// process provisions its tenant once and only logs in again when the bearer
// expires. The mutex is held across enrollment so concurrent tests cannot
// enroll twice.
type principalRegistry struct {
	sync.Mutex
	name       string
	enroll     principalEnroller
	login      certLogin
	principals map[string]*enrolledPrincipal
}

func newPrincipalRegistry(name string, enroll principalEnroller, login certLogin) *principalRegistry {
	return &principalRegistry{
		name:       name,
		enroll:     enroll,
		login:      login,
		principals: make(map[string]*enrolledPrincipal),
	}
}

// loginWithPIVCA is the certLogin of the compose stack: the PIV login route,
// trusting the integration CA.
func loginWithPIVCA(ctx context.Context, baseURL string, certPEM, keyPEM []byte) (string, int, error) {
	return postLoginWithCert(ctx, baseURL, certPEM, keyPEM, pivCAFile())
}

// bearer returns the principal's bearer for the cluster at baseURL, enrolling
// the principal on first use and logging in again once the bearer nears
// expiry.
func (r *principalRegistry) bearer(ctx context.Context, baseURL string) (string, error) {
	return r.bearerAt(ctx, baseURL, baseURL)
}

// bearerAt returns the principal's bearer issued by the node at loginURL,
// enrolling the principal in the cluster at clusterURL on first use and
// logging in at loginURL again once that node's bearer nears expiry. A failed
// enrollment is not remembered, so the next call retries it.
func (r *principalRegistry) bearerAt(ctx context.Context, clusterURL, loginURL string) (string, error) {
	clusterURL = strings.TrimRight(clusterURL, "/")
	loginURL = strings.TrimRight(loginURL, "/")

	r.Lock()
	defer r.Unlock()

	principal, ok := r.principals[clusterURL]
	if !ok {
		enrolled, err := r.enroll(ctx, clusterURL)
		if err != nil {
			return "", err
		}
		principal = enrolled
		r.principals[clusterURL] = principal
	}
	if principal.tokens == nil {
		principal.tokens = make(map[string]pivCachedToken)
	}
	if token, ok := principal.tokens[loginURL]; ok && time.Now().Before(token.ExpiresAt) {
		return token.Bearer, nil
	}

	bearer, expiresIn, err := r.login(ctx, loginURL, principal.certPEM, principal.keyPEM)
	if err != nil {
		return "", &PrincipalAuthError{Principal: r.name, Step: "PIV login", TenantID: principal.tenantID, Cause: err}
	}
	principal.tokens[loginURL] = pivCachedToken{Bearer: bearer, ExpiresAt: bearerExpiry(expiresIn)}
	return bearer, nil
}

// loginURLFor returns the REST endpoint on the host target names, so a call
// carries a bearer the called node signed. A target on no listed node (a
// Unix socket, an unknown host, none) gets clusterURL.
func loginURLFor(clusterURL, target string) string {
	host := targetHostname(target)
	if host == "" {
		return clusterURL
	}
	for _, endpoint := range GetRESTEndpoints() {
		if u, err := url.Parse(endpoint); err == nil && u.Hostname() == host {
			return endpoint
		}
	}
	return clusterURL
}

// targetHostname returns the hostname of target: a URL such as gRPC's
// "https://test-node2:50051/pkg.Service", or a bare "host:port".
func targetHostname(target string) string {
	if u, err := url.Parse(target); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	if host, _, err := net.SplitHostPort(target); err == nil {
		return host
	}
	return target
}

// tenantID returns the tenant of the principal enrolled for baseURL, or ""
// before it is enrolled.
func (r *principalRegistry) tenantID(baseURL string) string {
	r.Lock()
	defer r.Unlock()
	principal, ok := r.principals[strings.TrimRight(baseURL, "/")]
	if !ok {
		return ""
	}
	return principal.tenantID
}

// release deletes the tenant of every enrolled principal and forgets each
// one it deleted, so a second release deletes nothing. A principal whose
// delete fails is kept and its failure returned as a *PrincipalAuthError.
func (r *principalRegistry) release(ctx context.Context, deleteTenant tenantDeleter) error {
	r.Lock()
	defer r.Unlock()

	var errs []error
	for baseURL, principal := range r.principals {
		if err := deleteTenant(ctx, baseURL, principal.tenantID); err != nil {
			errs = append(errs, &PrincipalAuthError{Principal: r.name, Step: "delete tenant", TenantID: principal.tenantID, Cause: err})
			continue
		}
		delete(r.principals, baseURL)
	}
	return errors.Join(errs...)
}

// mustBearer returns the principal's bearer for baseURL or fails the test.
func (r *principalRegistry) mustBearer(t *testing.T, baseURL string) string {
	t.Helper()
	bearer, err := r.bearer(context.Background(), baseURL)
	if err != nil {
		t.Fatalf("%s bearer for %q: %v", r.name, baseURL, err)
	}
	return bearer
}

// newTokenSource enrolls the principal of the first REST endpoint's cluster
// up front, so a provisioning failure surfaces with its cause rather than
// later as a refused call, and then returns, per call, the bearer of the node
// the call goes to.
func (r *principalRegistry) newTokenSource(ctx context.Context) (dataplane.TokenSource, error) {
	baseURL := GetRESTEndpoints()[0]
	if _, err := r.bearer(ctx, baseURL); err != nil {
		return nil, err
	}
	return func(ctx context.Context) (string, error) {
		return r.bearerAt(ctx, baseURL, loginURLFor(baseURL, dataplane.Target(ctx)))
	}, nil
}

// tokenSource is newTokenSource for a test, failing it on error.
func (r *principalRegistry) tokenSource(t *testing.T) dataplane.TokenSource {
	t.Helper()
	source, err := r.newTokenSource(context.Background())
	if err != nil {
		t.Fatalf("%s token source: %v", r.name, err)
	}
	return source
}

// grpcCredentials returns per-RPC credentials that send the principal's
// bearer on every gRPC call, over TCP or the Unix socket alike.
func (r *principalRegistry) grpcCredentials(t *testing.T) credentials.PerRPCCredentials {
	t.Helper()
	creds, err := r.newGRPCCredentials(context.Background())
	if err != nil {
		t.Fatalf("%s gRPC credentials: %v", r.name, err)
	}
	return creds
}

// newGRPCCredentials is grpcCredentials returning its error, for callers
// off the test goroutine, where t.Fatalf would exit through runtime.Goexit
// and skip the caller's cleanup.
func (r *principalRegistry) newGRPCCredentials(ctx context.Context) (credentials.PerRPCCredentials, error) {
	source, err := r.newTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	creds, err := dataplane.NewBearerCredentials(source)
	if err != nil {
		return nil, err
	}
	return creds, nil
}

// newHTTPClient returns NewHTTPClient's client with every request carrying
// the principal's bearer.
func (r *principalRegistry) newHTTPClient(ctx context.Context, timeout time.Duration) (*http.Client, error) {
	source, err := r.newTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	client := NewHTTPClient(timeout)
	transport, err := dataplane.NewBearerTransport(source, client.Transport)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: r.name, Step: "HTTP client", Cause: err}
	}
	client.Transport = transport
	return client, nil
}

// httpClient is newHTTPClient for a test, failing it on error.
func (r *principalRegistry) httpClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	client, err := r.newHTTPClient(context.Background(), timeout)
	if err != nil {
		t.Fatalf("%s HTTP client: %v", r.name, err)
	}
	return client
}

// bearerExpiry refreshes a minute before the server's expiry, or after 15
// minutes when the login response states none.
func bearerExpiry(expiresIn int) time.Time {
	if expiresIn > 60 {
		return time.Now().Add(time.Duration(expiresIn-60) * time.Second)
	}
	return time.Now().Add(15 * time.Minute)
}

// PrincipalNameError reports a principal name that cannot name a tenant: it
// is empty, or holds a character other than a lowercase letter, a digit or a
// hyphen.
type PrincipalNameError struct {
	// Name is the refused name.
	Name string
}

func (e *PrincipalNameError) Error() string {
	return "principal name " + strconv.Quote(e.Name) + " must be non-empty lowercase letters, digits and hyphens"
}

// validPrincipalName reports whether name can be part of a tenant ID.
func validPrincipalName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// namedPrincipalRegistries holds one principalRegistry per caller-chosen
// name, so a test can act as several principals, each in its own tenant.
type namedPrincipalRegistries struct {
	sync.Mutex
	newRegistry func(name string) *principalRegistry
	registries  map[string]*principalRegistry
}

func newNamedPrincipalRegistries(newRegistry func(name string) *principalRegistry) *namedPrincipalRegistries {
	return &namedPrincipalRegistries{
		newRegistry: newRegistry,
		registries:  make(map[string]*principalRegistry),
	}
}

// registry returns the registry of the principal called name, creating it on
// first use.
func (n *namedPrincipalRegistries) registry(name string) (*principalRegistry, error) {
	if !validPrincipalName(name) {
		return nil, &PrincipalNameError{Name: name}
	}

	n.Lock()
	defer n.Unlock()

	registry, ok := n.registries[name]
	if !ok {
		registry = n.newRegistry(name)
		n.registries[name] = registry
	}
	return registry, nil
}

// mustRegistry is registry for a test, failing it on a refused name.
func (n *namedPrincipalRegistries) mustRegistry(t *testing.T, name string) *principalRegistry {
	t.Helper()
	registry, err := n.registry(name)
	if err != nil {
		t.Fatalf("named principal: %v", err)
	}
	return registry
}

// release deletes the tenant of every named principal, as
// principalRegistry.release does for one.
func (n *namedPrincipalRegistries) release(ctx context.Context, deleteTenant tenantDeleter) error {
	n.Lock()
	defer n.Unlock()

	var errs []error
	for _, registry := range n.registries {
		errs = append(errs, registry.release(ctx, deleteTenant))
	}
	return errors.Join(errs...)
}

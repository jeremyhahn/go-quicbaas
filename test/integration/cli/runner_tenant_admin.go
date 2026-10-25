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

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// tenantAdminLoginTimeout bounds provisioning the tenant and logging its
// admin in.
const tenantAdminLoginTimeout = 30 * time.Second

// systemAdminCommands names the CLI command groups that are the system
// admin's control plane. They keep running under the system admin after
// ActAsTenantAdmin, so a test can still provision and delete its tenant.
var systemAdminCommands = map[string]bool{
	"iam tenant": true,
}

// isSystemAdminCommand reports whether args invoke a system-admin
// control-plane command group.
func isSystemAdminCommand(args []string) bool {
	if len(args) < 2 {
		return false
	}
	return systemAdminCommands[args[0]+" "+args[1]]
}

// TenantAdminModeError reports a step of ActAsTenantAdmin that failed.
type TenantAdminModeError struct {
	Step     string
	TenantID string
	Cause    error
}

func (e *TenantAdminModeError) Error() string {
	return "tenant-admin mode for " + e.TenantID + ": " + e.Step + ": " + e.Cause.Error()
}

func (e *TenantAdminModeError) Unwrap() error { return e.Cause }

// MissingRESTEndpointError reports that no REST endpoint is configured to
// provision the tenant through.
type MissingRESTEndpointError struct{}

func (e *MissingRESTEndpointError) Error() string { return "no REST endpoints configured" }

// tenantAdminSession is the tenant admin's credentials as the CLI reads them:
// its client certificate and key as files, and its bearer as the session
// token under a HOME of its own.
type tenantAdminSession struct {
	tenantID  string
	certPath  string
	keyPath   string
	home      string
	tokenPath string

	// admin is the provisioned tenant's admin; nil until the first command
	// that reaches the server.
	admin *helpers.TenantAdminSession

	// bearer is the bearer last written to tokenPath.
	bearer string
}

func newTenantAdminSession(dir, tenantID string) *tenantAdminSession {
	home := filepath.Join(dir, "home")
	return &tenantAdminSession{
		tenantID:  tenantID,
		certPath:  filepath.Join(dir, "tenant-admin.crt"),
		keyPath:   filepath.Join(dir, "tenant-admin.key"),
		home:      home,
		tokenPath: filepath.Join(home, ".quicbaas", "token"),
	}
}

// args are the global flags that make the CLI present the admin certificate.
func (s *tenantAdminSession) args() []string {
	return []string{"--tls-cert", s.certPath, "--tls-key", s.keyPath}
}

// env points HOME at the session's own directory, so the CLI reads the
// admin's session token rather than the system admin's. The rest of the
// environment is inherited.
func (s *tenantAdminSession) env() []string {
	return append(os.Environ(), "HOME="+s.home)
}

// ActAsTenantAdmin makes the runner act as the admin of tenantID, as a
// production caller does: the system admin only provisions the tenant
// (helpers.TenantAdmin, control plane), and every tenant-scoped command
// then runs under the tenant admin's certificate and bearer on whichever
// transport this runner drives. System-admin control-plane commands
// (systemAdminCommands) still run under the system admin.
//
// The tenant is provisioned just before the first command that this
// transport does not skip, so the test does not create it first, and a
// transport that skips every command leaves no tenant behind. The tenant is
// deleted when the test ends.
func (r *TestRunner) ActAsTenantAdmin(tenantID string) {
	r.t.Helper()
	r.tenantAdmin = newTenantAdminSession(r.t.TempDir(), tenantID)
}

// startTenantAdminSession provisions tenantID now, deleting it when t ends,
// and returns its admin's session for runCommandAs. It calls t.Fatal on any
// failure.
func startTenantAdminSession(t *testing.T, tenantID string) *tenantAdminSession {
	t.Helper()
	session := newTenantAdminSession(t.TempDir(), tenantID)
	if err := session.prepare(t); err != nil {
		t.Fatal(err)
	}
	return session
}

// prepare provisions the tenant on first use, deleting it when t ends, and
// makes sure the session token the CLI reads is a bearer that has not neared
// expiry, so a test that runs longer than one access-token lifetime stays
// authenticated.
func (s *tenantAdminSession) prepare(t *testing.T) error {
	if s.admin == nil {
		restEndpoints := helpers.GetRESTEndpoints()
		if len(restEndpoints) == 0 {
			return &TenantAdminModeError{Step: "find REST endpoint", TenantID: s.tenantID, Cause: &MissingRESTEndpointError{}}
		}
		admin := helpers.TenantAdmin(t, restEndpoints[0], s.tenantID)
		tenant := admin.Tenant()
		if err := os.WriteFile(s.certPath, tenant.AdminCertPEM, 0o600); err != nil {
			return &TenantAdminModeError{Step: "write admin certificate", TenantID: s.tenantID, Cause: err}
		}
		if err := os.WriteFile(s.keyPath, tenant.AdminKeyPEM, 0o600); err != nil {
			return &TenantAdminModeError{Step: "write admin key", TenantID: s.tenantID, Cause: err}
		}
		s.admin = admin
	}

	ctx, cancel := context.WithTimeout(context.Background(), tenantAdminLoginTimeout)
	defer cancel()

	bearer, expiresAt, err := s.admin.BearerSession(ctx)
	if err != nil {
		return &TenantAdminModeError{Step: "tenant admin login", TenantID: s.tenantID, Cause: err}
	}
	if bearer == s.bearer {
		return nil
	}
	if err := helpers.WriteCLISession(s.tokenPath, bearer, expiresAt); err != nil {
		return &TenantAdminModeError{Step: "write admin session", TenantID: s.tenantID, Cause: err}
	}
	s.bearer = bearer
	return nil
}

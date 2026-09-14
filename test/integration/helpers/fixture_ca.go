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

// fixture_ca.go is the suite's single handle on the fixture certificate
// authority.
//
// Every runtime credential the integration suite needs — a tenant-scoped client
// certificate for AuthTenantAdmin, a software PIV slot certificate for the login
// matrix — is issued by this authority, which is QuicBaaS's own CA reached
// through the adapter the daemon serves certificate operations through. Until
// go-quicbaas#943 these helpers loaded the fixture CA's PRIVATE KEY off the
// mounted volume and called x509.CreateCertificate themselves, so the suite
// authenticated with certificates built by test code rather than by the
// product.

package helpers

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/test/integration/certfixture"
)

// IntegrationTrustDomain is the SPIFFE trust domain the fixture CA stamps into
// tenant-scoped client certificates.
//
// It MUST equal spiffe_trust_domain in
// test/integration/certfixture/topology.json, and — whenever a node
// configuration pins iam.spiffe_trust_domain — that value too: login compares
// the configured domain against the presented one, so a mismatch fails every
// login closed. The integration node configs currently pin nothing
// (go-quicbaas#930 reverted the key back out), which means any domain is
// accepted; certfixture's own suite asserts the agreement so a pin coming back
// with a different value is a test failure rather than a cluster-wide login
// outage.
const IntegrationTrustDomain = "quicbaas.integration"

var (
	fixtureAuthorityOnce sync.Once
	fixtureAuthority     *certfixture.Authority
	fixtureAuthorityErr  error
)

// FixtureCertsDir returns the directory holding the integration fixture
// material, including the CA's own key and certificate stores under
// ca-keystore/. QUICBAAS_TEST_CERTS_DIR overrides it; otherwise it is the
// directory the trust anchor lives in, so one mount point configures both.
func FixtureCertsDir() string {
	if v := os.Getenv("QUICBAAS_TEST_CERTS_DIR"); v != "" {
		return v
	}
	return filepath.Dir(IntegrationCAPath)
}

// FixtureAuthority opens the fixture CA once and returns a handle that issues
// through the product's CA adapter.
//
// The authority is opened, not created: the harness issued the cluster's
// material with the same CA before the nodes started, so this adopts that root
// and everything it signs chains to the trust anchor the nodes already loaded.
func FixtureAuthority(ctx context.Context) (*certfixture.Authority, error) {
	fixtureAuthorityOnce.Do(func() {
		fixtureAuthority, fixtureAuthorityErr = certfixture.Open(ctx, certfixture.Config{
			Dir:               FixtureCertsDir(),
			SPIFFETrustDomain: IntegrationTrustDomain,
		})
	})
	return fixtureAuthority, fixtureAuthorityErr
}

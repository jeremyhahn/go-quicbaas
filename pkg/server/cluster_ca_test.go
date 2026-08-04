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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// clusterACMEServer builds a minimal *Server whose config enables cluster ACME
// enrollment, for exercising the (untagged) cluster-CA helpers.
func clusterACMEServer(mutate func(*config.Config)) *Server {
	cfg := &config.Config{}
	cfg.Server.Mode = config.ServerModeCluster
	cfg.Raft.RaftEnrollment.ACME.Enabled = true
	cfg.CA.RootCommonName = "QuicBaaS Root CA"
	cfg.CA.RootOrganization = "QuicBaaS"
	if mutate != nil {
		mutate(cfg)
	}
	return &Server{config: cfg}
}

// TestClusterACMEPredicates verifies the gating predicates that keep non-cluster
// and joiner deployments on the existing single-root CA path.
func TestClusterACMEPredicates(t *testing.T) {
	// First node: cluster mode, ACME enabled.
	first := clusterACMEServer(nil)
	if !first.clusterACMEEnabled() {
		t.Error("clusterACMEEnabled() = false, want true for cluster+ACME")
	}

	// Standalone mode → cluster ACME disabled regardless of the ACME flag.
	standalone := clusterACMEServer(func(c *config.Config) {
		c.Server.Mode = config.ServerModeStandalone
	})
	if standalone.clusterACMEEnabled() {
		t.Error("clusterACMEEnabled() = true in standalone mode")
	}
}

// TestClusterCACommonName verifies the dedicated-intermediate CN resolution:
// explicit config wins, else a default derived from the root CN.
func TestClusterCACommonName(t *testing.T) {
	def := clusterACMEServer(nil)
	if got, want := def.clusterCACommonName(), "QuicBaaS Root CA Cluster CA"; got != want {
		t.Errorf("default clusterCACommonName() = %q, want %q", got, want)
	}

	explicit := clusterACMEServer(func(c *config.Config) {
		c.Raft.RaftEnrollment.ClusterCACN = "Prod Cluster Intermediate"
	})
	if got, want := explicit.clusterCACommonName(), "Prod Cluster Intermediate"; got != want {
		t.Errorf("explicit clusterCACommonName() = %q, want %q", got, want)
	}
}

// TestBuildClusterCAMultiIdentityConfig verifies the CA hierarchy has the root
// plus a dedicated cluster intermediate signed by the root (ParentCA), with the
// root remaining the issuing identity (SelectedCA 0) so ordinary certs still
// issue from the root.
func TestBuildClusterCAMultiIdentityConfig(t *testing.T) {
	s := clusterACMEServer(func(c *config.Config) {
		c.Storage.DataDir = t.TempDir()
	})

	cfg := s.buildClusterCAMultiIdentityConfig("software")
	if len(cfg.Identity) != 2 {
		t.Fatalf("expected 2 identities (root + cluster intermediate), got %d", len(cfg.Identity))
	}
	root := cfg.Identity[0]
	if !root.IsRoot {
		t.Error("identity[0] should be the root")
	}
	intermediate := cfg.Identity[1]
	if intermediate.IsRoot {
		t.Error("identity[1] (cluster intermediate) must not be a root")
	}
	if intermediate.Subject.CommonName != "QuicBaaS Root CA Cluster CA" {
		t.Errorf("cluster intermediate CN = %q", intermediate.Subject.CommonName)
	}
	if intermediate.ParentCA != root.Subject.CommonName {
		t.Errorf("cluster intermediate ParentCA = %q, want root CN %q",
			intermediate.ParentCA, root.Subject.CommonName)
	}
	if cfg.SelectedCA != 0 {
		t.Errorf("SelectedCA = %d, want 0 (root remains the issuing identity)", cfg.SelectedCA)
	}
}

// TestBuildClusterPreSignHooks_Gated verifies the SPIFFE URI-SAN hook registry
// is nil when cluster ACME is off and registers exactly one hook when on.
func TestBuildClusterPreSignHooks_Gated(t *testing.T) {
	off := clusterACMEServer(func(c *config.Config) {
		c.Server.Mode = config.ServerModeStandalone
	})
	if off.buildClusterPreSignHooks() != nil {
		t.Error("expected nil pre-sign hooks when cluster ACME is off")
	}
	if off.clusterACMEIssuerCN() != "" {
		t.Error("expected empty issuer CN when cluster ACME is off")
	}

	on := clusterACMEServer(nil)
	registry := on.buildClusterPreSignHooks()
	if registry == nil {
		t.Fatal("expected a non-nil pre-sign hook registry when cluster ACME is on")
	}
	if got := registry.Count(); got != 1 {
		t.Errorf("registry.Count() = %d, want 1", got)
	}
	if on.clusterACMEIssuerCN() != "QuicBaaS Root CA Cluster CA" {
		t.Errorf("clusterACMEIssuerCN() = %q", on.clusterACMEIssuerCN())
	}
}

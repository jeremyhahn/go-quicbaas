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
	"encoding/asn1"
	"encoding/json"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/clusterenroll"
	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

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
	offRegistry, err := off.buildClusterPreSignHooks()
	if err != nil {
		t.Fatalf("buildClusterPreSignHooks (off): %v", err)
	}
	if offRegistry != nil {
		t.Error("expected nil pre-sign hooks when cluster ACME is off")
	}
	if off.clusterACMEIssuerCN() != "" {
		t.Error("expected empty issuer CN when cluster ACME is off")
	}

	on := clusterACMEServer(func(c *config.Config) {
		c.Raft.RaftAddress = "10.0.0.1:7000"
	})
	registry, err := on.buildClusterPreSignHooks()
	if err != nil {
		t.Fatalf("buildClusterPreSignHooks (on): %v", err)
	}
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

// clusterHookOrder builds an ACME order carrying one identifier. The xkms SDK
// does not re-export the identifier type, so the order is decoded from its
// RFC 8555 JSON form.
func clusterHookOrder(t *testing.T, idType, value string) *xkmsacme.ACMEOrderEntity {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"identifiers": []map[string]string{{"type": idType, "value": value}},
	})
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	order := &xkmsacme.ACMEOrderEntity{}
	if err := json.Unmarshal(body, order); err != nil {
		t.Fatalf("unmarshal order: %v", err)
	}
	return order
}

// sanURIs decodes the uniformResourceIdentifier GeneralNames (tag 6) of a
// SubjectAltName extension value.
func sanURIs(t *testing.T, value []byte) []string {
	t.Helper()
	var names asn1.RawValue
	if _, err := asn1.Unmarshal(value, &names); err != nil {
		t.Fatalf("unmarshal GeneralNames: %v", err)
	}
	var uris []string
	for rest := names.Bytes; len(rest) > 0; {
		var name asn1.RawValue
		var err error
		rest, err = asn1.Unmarshal(rest, &name)
		if err != nil {
			t.Fatalf("unmarshal GeneralName: %v", err)
		}
		if name.Class == asn1.ClassContextSpecific && name.Tag == 6 {
			uris = append(uris, string(name.Bytes))
		}
	}
	return uris
}

// TestBuildClusterPreSignHooks_StampsListedMember verifies the registered hook
// stamps the SPIFFE node URI only for an enroll-01-validated TCG-CSR order
// whose permanent-identifier is a listed cluster member.
func TestBuildClusterPreSignHooks_StampsListedMember(t *testing.T) {
	const member = "10.0.0.2:7000"
	srv := clusterACMEServer(func(c *config.Config) {
		c.Raft.RaftAddress = "10.0.0.1:7000"
		c.Raft.JoinAddresses = []string{"1=10.0.0.1:7000", "2=" + member}
		c.Raft.RaftEnrollment.TrustDomain = "test.cluster"
	})
	registry, err := srv.buildClusterPreSignHooks()
	if err != nil {
		t.Fatalf("buildClusterPreSignHooks: %v", err)
	}

	exts, err := registry.Execute(&xkmsacme.ACMEOrderPreSignHookContext{
		Context:             context.Background(),
		Order:               clusterHookOrder(t, xkmsacme.ACMEAuthzTypePermanentIdentifier, member),
		IsTCGCSR:            true,
		ValidatedChallenges: []string{xkmsacme.ACMEChallengeTypeEnroll01},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(exts) != 1 {
		t.Fatalf("got %d extensions, want 1", len(exts))
	}
	if exts[0].Critical {
		t.Error("SubjectAltName extension must not be critical")
	}
	want := clusterenroll.NodeURI("test.cluster", member)
	if got := sanURIs(t, exts[0].Value); len(got) != 1 || got[0] != want {
		t.Errorf("SAN URIs = %v, want [%s]", got, want)
	}
}

// TestBuildClusterPreSignHooks_RefusesUnqualifiedOrders verifies the hook
// stamps nothing for an unlisted permanent-identifier, a non-enroll-01
// validation, or a non-TCG CSR.
func TestBuildClusterPreSignHooks_RefusesUnqualifiedOrders(t *testing.T) {
	const member = "10.0.0.1:7000"
	srv := clusterACMEServer(func(c *config.Config) {
		c.Raft.RaftAddress = member
	})
	registry, err := srv.buildClusterPreSignHooks()
	if err != nil {
		t.Fatalf("buildClusterPreSignHooks: %v", err)
	}

	tests := []struct {
		name       string
		value      string
		isTCGCSR   bool
		challenges []string
	}{
		{"unlisted permanent-identifier", "10.0.0.9:7000", true, []string{xkmsacme.ACMEChallengeTypeEnroll01}},
		{"dns-01 validation", member, true, []string{xkmsacme.ACMEChallengeTypeDNS01}},
		{"enroll-01 plus another challenge", member, true, []string{xkmsacme.ACMEChallengeTypeEnroll01, xkmsacme.ACMEChallengeTypeDeviceAttest01}},
		{"no validated challenge", member, true, nil},
		{"not a TCG CSR", member, false, []string{xkmsacme.ACMEChallengeTypeEnroll01}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exts, err := registry.Execute(&xkmsacme.ACMEOrderPreSignHookContext{
				Context:             context.Background(),
				Order:               clusterHookOrder(t, xkmsacme.ACMEAuthzTypePermanentIdentifier, tt.value),
				IsTCGCSR:            tt.isTCGCSR,
				ValidatedChallenges: tt.challenges,
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(exts) != 0 {
				t.Errorf("got %d extensions, want none", len(exts))
			}
		})
	}
}

// TestBuildClusterPreSignHooks_EmptyMembership pins the empty-membership
// behaviour: buildClusterPreSignHooks refuses a cluster with no raft address,
// and the upstream hook given no members stamps nothing.
func TestBuildClusterPreSignHooks_EmptyMembership(t *testing.T) {
	srv := clusterACMEServer(nil)
	if _, err := srv.buildClusterPreSignHooks(); err == nil {
		t.Fatal("expected an error for a cluster with no raft address")
	}

	hook := xkmsacme.NewACMEClusterNodeURISANHook(defaultRaftTrustDomain, map[uint64]string{})
	exts, err := hook(&xkmsacme.ACMEOrderPreSignHookContext{
		Context:             context.Background(),
		Order:               clusterHookOrder(t, xkmsacme.ACMEAuthzTypePermanentIdentifier, "10.0.0.1:7000"),
		IsTCGCSR:            true,
		ValidatedChallenges: []string{xkmsacme.ACMEChallengeTypeEnroll01},
	})
	if err != nil {
		t.Fatalf("hook: %v", err)
	}
	if len(exts) != 0 {
		t.Errorf("got %d extensions from empty membership, want none", len(exts))
	}
}

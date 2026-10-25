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

package certfixture

import (
	"errors"
	"testing"
)

func TestSPIFFEName(t *testing.T) {
	cases := map[string]struct {
		commonName string
		want       string
	}{
		"email keeps its local part":        {"admin@integration.test", "admin"},
		"only the first @ splits":           {"a@b@c", "a"},
		"a node name is already valid":      {"test-node1", "test-node1"},
		"dots and underscores are kept":     {"svc.agent_01", "svc.agent_01"},
		"invalid characters become hyphens": {"jane doe+ops/x@example.test", "jane-doe-ops-x"},
		"non-ASCII becomes one hyphen each": {"zoë@example.test", "zo-"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := SPIFFEName(tc.commonName); got != tc.want {
				t.Errorf("SPIFFEName(%q) = %q, want %q", tc.commonName, got, tc.want)
			}
		})
	}
}

func TestSpiffeURISAN(t *testing.T) {
	got, err := spiffeURISAN("system", "user", "admin@integration.test")
	if err != nil {
		t.Fatalf("spiffeURISAN: %v", err)
	}
	if want := "URI:spiffe://system/user/admin"; got != want {
		t.Errorf("spiffeURISAN = %q, want %q", got, want)
	}
}

func TestSpiffeURISAN_RefusesAnEmptyName(t *testing.T) {
	// "@integration.test" has an empty local part, so there is no name
	// segment to put in the ID.
	if got, err := spiffeURISAN("system", "user", "@integration.test"); err == nil {
		t.Fatalf("spiffeURISAN accepted an empty name and returned %q", got)
	}
}

func TestTopologyValidate_RefusesASPIFFENameCollision(t *testing.T) {
	node := ServerFixture{File: "node1", CommonName: "test-node1"}
	cases := map[string]struct {
		topo  Topology
		first string
		next  string
	}{
		"two users of one tenant": {
			topo: Topology{
				SPIFFETrustDomain: trustDomain,
				Nodes:             []ServerFixture{node},
				Clients: []ClientFixture{
					{File: "a", CommonName: "admin@one.test", TenantID: "system"},
					{File: "b", CommonName: "admin@two.test", TenantID: "system"},
				},
			},
			first: "admin@one.test",
			next:  "admin@two.test",
		},
		"two nodes whose names differ only in invalid characters": {
			topo: Topology{
				SPIFFETrustDomain: trustDomain,
				Nodes: []ServerFixture{
					{File: "n1", CommonName: "test node"},
					{File: "n2", CommonName: "test+node"},
				},
			},
			first: "test node",
			next:  "test+node",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var collision *SPIFFENameCollisionError
			if err := tc.topo.Validate(); !errors.As(err, &collision) {
				t.Fatalf("Validate error = %v, want a *SPIFFENameCollisionError", err)
			}
			if collision.First != tc.first || collision.Second != tc.next {
				t.Errorf("collision names %q and %q, want %q and %q", collision.First, collision.Second, tc.first, tc.next)
			}
		})
	}
}

func TestTopologyValidate_AllowsOneSPIFFENameInDistinctScopes(t *testing.T) {
	// The same name is distinct IDs under another tenant or another kind.
	topo := Topology{
		SPIFFETrustDomain: trustDomain,
		Nodes:             []ServerFixture{{File: "node1", CommonName: "admin"}},
		Clients: []ClientFixture{
			{File: "a", CommonName: "admin@one.test", TenantID: "system"},
			{File: "b", CommonName: "admin@two.test", TenantID: "acme"},
		},
	}
	if err := topo.Validate(); err != nil {
		t.Fatalf("Validate refused distinct IDs: %v", err)
	}
}

func TestSPIFFENameCollisionError_NamesBothFixtures(t *testing.T) {
	err := &SPIFFENameCollisionError{TenantID: "system", Kind: "user", Name: "admin", First: "admin@one.test", Second: "admin@two.test"}
	want := "certfixture: admin@one.test and admin@two.test both derive spiffe://system/user/admin"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

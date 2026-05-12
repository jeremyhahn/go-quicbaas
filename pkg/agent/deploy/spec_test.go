// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package deploy

import "testing"

func TestSpecResolvedDefaults(t *testing.T) {
	cases := []struct {
		name    string
		spec    *Spec
		wantIns string
		wantDat string
	}{
		{
			name:    "nil spec yields canonical defaults",
			spec:    nil,
			wantIns: ".quicbaas/bin",
			wantDat: ".quicbaas/agent",
		},
		{
			name:    "empty values yield canonical defaults",
			spec:    &Spec{},
			wantIns: ".quicbaas/bin",
			wantDat: ".quicbaas/agent",
		},
		{
			name: "explicit values are preserved",
			spec: &Spec{
				InstallDir: "opt/qb/bin",
				DataDir:    "var/qb/state",
			},
			wantIns: "opt/qb/bin",
			wantDat: "var/qb/state",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.spec.ResolvedInstallDir(); got != tc.wantIns {
				t.Fatalf("install dir: got %q want %q", got, tc.wantIns)
			}
			if got := tc.spec.ResolvedDataDir(); got != tc.wantDat {
				t.Fatalf("data dir: got %q want %q", got, tc.wantDat)
			}
		})
	}
}

func TestSpecSystemdToggle(t *testing.T) {
	t.Run("nil systemd not explicit", func(t *testing.T) {
		s := &Spec{}
		if s.SystemdExplicit() {
			t.Fatal("expected SystemdExplicit false for nil Systemd")
		}
		if s.UseSystemd() {
			t.Fatal("expected UseSystemd false for nil Systemd")
		}
	})
	t.Run("true is honoured", func(t *testing.T) {
		v := true
		s := &Spec{Systemd: &v}
		if !s.SystemdExplicit() || !s.UseSystemd() {
			t.Fatal("expected both accessors true")
		}
	})
	t.Run("false is explicit but falsy", func(t *testing.T) {
		v := false
		s := &Spec{Systemd: &v}
		if !s.SystemdExplicit() {
			t.Fatal("expected SystemdExplicit true")
		}
		if s.UseSystemd() {
			t.Fatal("expected UseSystemd false")
		}
	})
	t.Run("nil receiver safe", func(t *testing.T) {
		var s *Spec
		if s.UseSystemd() {
			t.Fatal("nil receiver must be false")
		}
		if s.SystemdExplicit() {
			t.Fatal("nil receiver must be false")
		}
	})
}

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

import (
	"errors"
	"strings"
	"testing"
)

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

func TestSpecDefaultNodeBind(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want string
	}{
		{name: "ssh host", spec: Spec{Host: "node01"}, want: "node01:9091"},
		{name: "ssh host with port and user", spec: Spec{Host: "alice@node01:2222"}, want: "node01:9091"},
		{name: "ipv6 host", spec: Spec{Host: "[2001:db8::1]:22"}, want: "[2001:db8::1]:9091"},
		{name: "local advertised host", spec: Spec{Host: "node02.internal", Local: true}, want: "node02.internal:9091"},
		{name: "explicit bind wins", spec: Spec{Host: "node01", NodeBind: "0.0.0.0:9191"}, want: "0.0.0.0:9191"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := tc.spec
			if err := spec.DefaultNodeBind(); err != nil {
				t.Fatalf("DefaultNodeBind: %v", err)
			}
			if spec.NodeBind != tc.want {
				t.Fatalf("NodeBind = %q, want %q", spec.NodeBind, tc.want)
			}
		})
	}
}

func TestSpecDefaultNodeBindNeverWildcard(t *testing.T) {
	for _, host := range []string{"", "0.0.0.0", "alice@[::]:22"} {
		spec := Spec{Host: host}

		err := spec.DefaultNodeBind()

		var invalid *InvalidSpecError
		if !errors.As(err, &invalid) || invalid.Field != "node_bind" {
			t.Fatalf("host %q: want InvalidSpecError on node_bind, got %v", host, err)
		}
		if spec.NodeBind != "" {
			t.Fatalf("host %q: defaulted to %q", host, spec.NodeBind)
		}
	}
}

func TestHostname(t *testing.T) {
	cases := map[string]string{
		"node01":                 "node01",
		"alice@node01:2222":      "node01",
		"[2001:db8::1]:22":       "2001:db8::1",
		"alice@[2001:db8::1]:22": "2001:db8::1",
		"":                       "",
	}
	for in, want := range cases {
		if got := Hostname(in); got != want {
			t.Errorf("Hostname(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSpecLocalSSHConflict(t *testing.T) {
	if err := (&Spec{Local: true}).LocalSSHConflict(); err != nil {
		t.Fatalf("a spec with no ssh settings conflicts: %v", err)
	}
	cases := map[string]Spec{
		"ssh_user":          {SSHUser: "alice"},
		"ssh_key_name":      {SSHKeyName: "alice-laptop"},
		"ssh_password":      {SSHPassword: "secret"},
		"insecure_host_key": {InsecureHostKey: true},
	}
	for field, spec := range cases {
		err := spec.LocalSSHConflict()

		var conflict *LocalSSHConflictError
		if !errors.As(err, &conflict) || conflict.Field != field {
			t.Fatalf("%s: want LocalSSHConflictError, got %v", field, err)
		}
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("%s: error does not name the field: %v", field, err)
		}
	}
}

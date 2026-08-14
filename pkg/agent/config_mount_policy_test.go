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

package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestConfig_Validate_AllowedMountRoots covers both halves of the new key:
// absolute roots are accepted, and anything that could never match a container
// mount source fails the config at startup instead of silently granting
// nothing.
func TestConfig_Validate_AllowedMountRoots(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		roots      []string
		wantError  bool
		wantDetail string
	}{
		{name: "unset is valid and denies everything", roots: nil},
		{name: "absolute roots accepted", roots: []string{"/srv/workspaces", "/opt/models"}},
		{
			name:       "relative root rejected",
			roots:      []string{"/srv/ok", "workspaces"},
			wantError:  true,
			wantDetail: "allowed_mount_roots[1]",
		},
		{
			name:       "empty root rejected",
			roots:      []string{""},
			wantError:  true,
			wantDetail: "allowed_mount_roots[0]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Config{
				ServerAddr:        "https://control.test:8443",
				TenantID:          "tenant-a",
				BindAddr:          "127.0.0.1:9091",
				WorkDir:           "/var/lib/quicbaas-agent",
				AgentCertFile:     "/etc/quicbaas/agent.crt",
				AgentKeyFile:      "/etc/quicbaas/agent.key",
				ServerCAFile:      "/etc/quicbaas/ca.crt",
				AllowedMountRoots: tc.roots,
			}
			err := cfg.Validate()
			if !tc.wantError {
				if err != nil {
					t.Fatalf("Validate: unexpected error %v", err)
				}
				return
			}
			if !errors.Is(err, ErrConfigMountRootNotAbsolute) {
				t.Fatalf("expected ErrConfigMountRootNotAbsolute, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Errorf("error %q does not name %q", err.Error(), tc.wantDetail)
			}
		})
	}
}

// TestConfig_MountPolicy_DeniesByDefault asserts the accessor's deny-by-default
// posture and that a configured root is carried through unchanged. A daemon
// owns no projects root, so tenant-scoped grants never apply to it.
func TestConfig_MountPolicy_DeniesByDefault(t *testing.T) {
	t.Parallel()

	empty := (&Config{}).MountPolicy()
	if empty == nil {
		t.Fatal("MountPolicy must never be nil: a nil policy is indistinguishable from an unwired runtime")
	}
	if empty.ProjectsRoot != "" {
		t.Errorf("projects root: got %q, want empty (a daemon manages no workspaces)", empty.ProjectsRoot)
	}
	if err := empty.AuthorizeSource("tenant-a", orchestration.MountFieldWorkDir, "/etc"); err == nil {
		t.Fatal("an unconfigured daemon must deny every mount source")
	}

	configured := (&Config{AllowedMountRoots: []string{"/srv/workspaces"}}).MountPolicy()
	if len(configured.AllowedRoots) != 1 || configured.AllowedRoots[0] != "/srv/workspaces" {
		t.Fatalf("allowed roots: got %v", configured.AllowedRoots)
	}
}

// TestMapRuntimeError_MountDeniedMapsToPermissionDenied pins the daemon-side
// wire code for a refused bind mount. The control plane's REST layer keys 403
// off exactly this code, so a FailedPrecondition or Internal here would resurface
// as an opaque 500 to the operator.
func TestMapRuntimeError_MountDeniedMapsToPermissionDenied(t *testing.T) {
	t.Parallel()

	err := &orchestration.MountNotAllowedError{
		Field:    orchestration.MountFieldWorkDir,
		TenantID: "tenant-a",
		Source:   "/etc",
		Reason:   "outside every allowed mount root",
	}
	got := mapRuntimeError(err)
	st, ok := status.FromError(got)
	if !ok {
		t.Fatalf("expected gRPC status, got %T: %v", got, got)
	}
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("code: got %v want PermissionDenied", st.Code())
	}
	if !strings.Contains(st.Message(), "/etc") {
		t.Errorf("message %q lost the offending source", st.Message())
	}
	if !strings.Contains(st.Message(), orchestration.MountFieldWorkDir) {
		t.Errorf("message %q lost the offending field", st.Message())
	}
}

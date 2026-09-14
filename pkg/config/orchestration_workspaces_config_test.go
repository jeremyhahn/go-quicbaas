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

package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOrchestrationConfig_EffectiveProjectsRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		projectsRoot string
		dataDir      string
		want         string
	}{
		{
			name:         "explicit root wins",
			projectsRoot: "/srv/projects",
			dataDir:      "/var/lib/quicbaas",
			want:         "/srv/projects",
		},
		{
			name:    "empty derives from data dir",
			dataDir: "/var/lib/quicbaas",
			want:    "/var/lib/quicbaas/projects",
		},
		{
			name: "empty root and empty data dir",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := OrchestrationConfig{ProjectsRoot: tt.projectsRoot}
			if got := cfg.EffectiveProjectsRoot(tt.dataDir); got != tt.want {
				t.Errorf("EffectiveProjectsRoot(%q) = %q, want %q", tt.dataDir, got, tt.want)
			}
		})
	}
}

// TestOrchestrationConfig_EffectiveSocketsRoot pins the derivation of the
// per-session agent-socket root. It is DELIBERATELY not derived from the
// projects root: a socket under a managed workspace exceeds the kernel's
// 107-byte sun_path budget on any realistic deployment (go-quicbaas#628).
func TestOrchestrationConfig_EffectiveSocketsRoot(t *testing.T) {
	instance := SocketsRootInstanceSegment("/var/lib/quicbaas")
	tests := []struct {
		name        string
		socketsRoot string
		runtimeDir  string
		dataDir     string
		want        string
	}{
		{
			name:        "explicit root wins over both",
			socketsRoot: "/run/quicbaas-sockets",
			runtimeDir:  "/run/user/1000",
			dataDir:     "/var/lib/quicbaas",
			want:        "/run/quicbaas-sockets",
		},
		{
			// The runtime directory is per-USER, so the derived root takes a
			// per-daemon segment under it (go-quicbaas#687).
			name:       "the runtime directory is preferred, one instance deep",
			runtimeDir: "/run/user/1000",
			dataDir:    "/var/lib/quicbaas",
			want:       "/run/user/1000/quicbaas/" + instance,
		},
		{
			name:    "no runtime directory derives from the data dir",
			dataDir: "/var/lib/quicbaas",
			want:    "/var/lib/quicbaas/s",
		},
		{
			name:       "a relative runtime directory is not a root",
			runtimeDir: "user/1000",
			dataDir:    "/var/lib/quicbaas",
			want:       "/var/lib/quicbaas/s",
		},
		{
			name: "nothing to derive from",
			want: "",
		},
		{
			// A daemon that stores nothing on disk has no identity to derive
			// a per-daemon root from, and the runtime directory alone is not
			// one. The empty answer is refused by the caller, which names the
			// setting; a shared root would be silent.
			name:       "a runtime directory without a data dir identifies no daemon",
			runtimeDir: "/run/user/1000",
			want:       "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_RUNTIME_DIR", tt.runtimeDir)
			cfg := OrchestrationConfig{SocketsRoot: tt.socketsRoot}
			got, err := cfg.EffectiveSocketsRoot(tt.dataDir)
			if err != nil {
				t.Fatalf("EffectiveSocketsRoot(%q): %v", tt.dataDir, err)
			}
			if got != tt.want {
				t.Errorf("EffectiveSocketsRoot(%q) = %q, want %q", tt.dataDir, got, tt.want)
			}
		})
	}
}

// TestOrchestrationConfig_SocketsRootIsPerDaemonNotPerUser is the
// go-quicbaas#687 regression.
//
// $XDG_RUNTIME_DIR is a per-USER directory. A root derived from it alone is
// reached by every quicbaasd the user runs, so two daemons with different
// data directories bound their session sockets under one root — and the
// startup sweep that removes the sessions a killed daemon left (#633) would
// be walking a sibling daemon's LIVE ones.
func TestOrchestrationConfig_SocketsRootIsPerDaemonNotPerUser(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	cfg := OrchestrationConfig{}

	first, err := cfg.EffectiveSocketsRoot("/srv/quicbaas/alpha/data")
	if err != nil {
		t.Fatalf("first daemon: %v", err)
	}
	second, err := cfg.EffectiveSocketsRoot("/srv/quicbaas/beta/data")
	if err != nil {
		t.Fatalf("second daemon: %v", err)
	}
	if first == second {
		t.Fatalf("two daemons with different data directories share the sockets root %q; "+
			"one daemon's startup sweep can reach the other's live signing sessions", first)
	}
	// Disjoint, not merely different: neither root may contain the other, or
	// a sweep of the outer one still descends into the inner one's sessions.
	if strings.HasPrefix(first+"/", second+"/") || strings.HasPrefix(second+"/", first+"/") {
		t.Fatalf("the two roots are nested: %q and %q", first, second)
	}

	// Stable across calls: the root is recomputed from the configuration on
	// every start and on every mount-policy check, and a root that moved
	// between two of them would strand every socket already bound.
	for i := 0; i < 3; i++ {
		again, againErr := cfg.EffectiveSocketsRoot("/srv/quicbaas/alpha/data")
		if againErr != nil {
			t.Fatalf("repeat %d: %v", i, againErr)
		}
		if again != first {
			t.Fatalf("repeat %d derived %q, first call derived %q", i, again, first)
		}
	}

	// The same data directory spelled unclean is the same daemon: the root
	// is compared against mount sources by absolute prefix, so two spellings
	// yielding two roots would deny the second daemon's own socket mount.
	unclean, err := cfg.EffectiveSocketsRoot("/srv/quicbaas/alpha/./data/")
	if err != nil {
		t.Fatalf("unclean spelling: %v", err)
	}
	if unclean != first {
		t.Fatalf("the same data directory spelled %q derived %q, want %q",
			"/srv/quicbaas/alpha/./data/", unclean, first)
	}
}

// TestOrchestrationConfig_SocketsRootIdentityIsTheAbsoluteDataDir proves the
// identity survives the working directory, which is what a RELATIVE data_dir
// makes it depend on. Two daemons started from different directories with the
// same `data_dir: data` are different daemons.
func TestOrchestrationConfig_SocketsRootIdentityIsTheAbsoluteDataDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	cfg := OrchestrationConfig{}

	absolute, err := filepath.Abs("data")
	if err != nil {
		t.Fatalf("resolving the test's own relative path: %v", err)
	}
	relative, err := cfg.EffectiveSocketsRoot("data")
	if err != nil {
		t.Fatalf("relative data dir: %v", err)
	}
	resolved, err := cfg.EffectiveSocketsRoot(absolute)
	if err != nil {
		t.Fatalf("absolute data dir: %v", err)
	}
	if relative != resolved {
		t.Fatalf("the relative data dir derived %q and its absolute form %q; "+
			"the identity is not the resolved directory", relative, resolved)
	}
	if elsewhere, elseErr := cfg.EffectiveSocketsRoot("/somewhere/else/data"); elseErr != nil {
		t.Fatalf("a third data dir: %v", elseErr)
	} else if elsewhere == relative {
		t.Fatalf("a data dir in another directory shares the root %q", relative)
	}
}

// TestOrchestrationConfig_SocketsRootStaysUnderTheKernelsBudget is the #628
// arithmetic re-checked against the segment #687 added.
//
// A unix socket address is a fixed-size kernel field: sun_path holds 108
// bytes including its terminator. The socket the sdlc signing agent binds is
// <root>/<12 hex>/agent.sock, so the derived root has to leave room for 25
// more bytes. The instance segment spends 13 of the budget and this asserts
// what is left is still enough for a realistic runtime directory.
func TestOrchestrationConfig_SocketsRootStaysUnderTheKernelsBudget(t *testing.T) {
	// sun_path, the session directory the agent mints, the socket file and
	// the two separators between them. Restated here rather than imported:
	// pkg/sdlc imports this package, so the budget cannot be read back from
	// the consumer that enforces it.
	const sunPath = 108
	const longestSocketPath = sunPath - 1
	const sessionDirLen = 12
	const socketFile = "agent.sock"

	// The largest uid Linux hands out, so no realistic /run/user is longer.
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/4294967294")
	cfg := OrchestrationConfig{}
	root, err := cfg.EffectiveSocketsRoot("/srv/quicbaas/a-deployment-with-a-long-name/data")
	if err != nil {
		t.Fatalf("EffectiveSocketsRoot: %v", err)
	}
	longest := len(root) + 1 + sessionDirLen + 1 + len(socketFile)
	if longest > longestSocketPath {
		t.Fatalf("the derived root %q (%d bytes) yields %d-byte socket paths, over the %d-byte limit",
			root, len(root), longest, longestSocketPath)
	}

	// The data-directory derivation takes no instance segment — it is already
	// inside the directory that distinguishes the daemons — so it must not
	// have grown either.
	t.Setenv("XDG_RUNTIME_DIR", "")
	fallback, err := cfg.EffectiveSocketsRoot("/var/lib/quicbaas")
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if fallback != "/var/lib/quicbaas/"+SocketsRootDataDirName {
		t.Fatalf("the data-directory derivation changed: %q", fallback)
	}
}

// TestOrchestrationConfig_SocketsRootInstanceSegmentShape pins what the
// segment is allowed to be: a fixed-width, path-safe name. A segment carrying
// a separator or a variable width would break both the path arithmetic and
// the sweep's name predicate in pkg/sdlc.
func TestOrchestrationConfig_SocketsRootInstanceSegmentShape(t *testing.T) {
	t.Parallel()

	for _, dataDir := range []string{"", ".", "/", "/var/lib/quicbaas", "relative/data",
		"/a/data dir/with spaces", "/data/with/../traversal", strings.Repeat("/d", 200)} {
		segment := SocketsRootInstanceSegment(dataDir)
		if len(segment) != SocketsRootInstanceLen {
			t.Errorf("segment for %q is %d bytes, want %d", dataDir, len(segment), SocketsRootInstanceLen)
		}
		if strings.Trim(segment, "0123456789abcdef") != "" {
			t.Errorf("segment for %q is %q, want lowercase hexadecimal only", dataDir, segment)
		}
		if filepath.Clean(segment) != segment {
			t.Errorf("segment for %q does not survive path cleaning: %q", dataDir, segment)
		}
	}
}

// TestOrchestrationConfig_SocketsRootIsIndependentOfTheProjectsRoot is the
// regression go-quicbaas#628 reported: the burn-in node's socket path was 148
// bytes because it grew with the projects root, the tenant and the project
// id. The derived sockets root must not move at all when the projects root
// does, whichever of the two derivations is in play.
func TestOrchestrationConfig_SocketsRootIsIndependentOfTheProjectsRoot(t *testing.T) {
	deep := "/" + strings.Repeat("p", 199)
	if len(deep) != 200 {
		t.Fatalf("the test's own projects root is %d bytes", len(deep))
	}
	dataDir := "/var/lib/quicbaas"

	for name, runtimeDir := range map[string]string{
		"with a runtime directory": "/run/user/1000",
		"without one":              "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
			shallow := OrchestrationConfig{ProjectsRoot: dataDir + "/projects"}
			nested := OrchestrationConfig{ProjectsRoot: deep}
			shallowRoot, err := shallow.EffectiveSocketsRoot(dataDir)
			if err != nil {
				t.Fatalf("shallow projects root: %v", err)
			}
			nestedRoot, err := nested.EffectiveSocketsRoot(dataDir)
			if err != nil {
				t.Fatalf("nested projects root: %v", err)
			}
			if shallowRoot != nestedRoot {
				t.Fatalf("a %d-byte projects root moved the sockets root: %q vs %q",
					len(deep), nestedRoot, shallowRoot)
			}
		})
	}
}

func TestValidateOrchestration_ProjectsRootAndMountRoots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*OrchestrationConfig)
		wantError string
	}{
		{
			name:   "absolute projects root is valid",
			mutate: func(c *OrchestrationConfig) { c.ProjectsRoot = "/srv/projects" },
		},
		{
			name:   "empty projects root is valid",
			mutate: func(c *OrchestrationConfig) { c.ProjectsRoot = "" },
		},
		{
			name:      "relative projects root is rejected",
			mutate:    func(c *OrchestrationConfig) { c.ProjectsRoot = "relative/projects" },
			wantError: "orchestration.projects_root",
		},
		{
			name:   "absolute sockets root is valid",
			mutate: func(c *OrchestrationConfig) { c.SocketsRoot = "/run/quicbaas" },
		},
		{
			name:      "relative sockets root is rejected",
			mutate:    func(c *OrchestrationConfig) { c.SocketsRoot = "relative/sockets" },
			wantError: "orchestration.sockets_root",
		},
		{
			name: "absolute mount roots are valid",
			mutate: func(c *OrchestrationConfig) {
				c.Sandbox.AllowedMountRoots = []string{"/data/shared", "/opt/models"}
			},
		},
		{
			name: "relative mount root is rejected",
			mutate: func(c *OrchestrationConfig) {
				c.Sandbox.AllowedMountRoots = []string{"/data/shared", "not/absolute"}
			},
			wantError: "orchestration.sandbox.allowed_mount_roots[1]",
		},
		{
			name: "empty mount root is rejected",
			mutate: func(c *OrchestrationConfig) {
				c.Sandbox.AllowedMountRoots = []string{""}
			},
			wantError: "orchestration.sandbox.allowed_mount_roots[0]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := OrchestrationConfig{}
			tt.mutate(&cfg)

			v := NewValidator()
			v.validateOrchestration(&cfg)

			if tt.wantError == "" {
				if len(v.errors) != 0 {
					t.Fatalf("expected no validation errors, got %v", v.errors)
				}
				return
			}
			if len(v.errors) == 0 {
				t.Fatalf("expected a validation error on %s, got none", tt.wantError)
			}
			found := false
			for _, e := range v.errors {
				if strings.Contains(e.Field, tt.wantError) {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected validation error on field %q, got %v", tt.wantError, v.errors)
			}
		})
	}
}

// TestValidateOrchestration_TrustPolicy pins the trust-policy vocabulary. An
// unrecognized value must FAIL the config: the fallback is the weakest policy,
// so a typo would silently disable the integrity floor the operator configured.
// It also guards the restatement of orchestration.ParseTrustPolicy's vocabulary
// in this package — a policy added there and not here would be rejected as a
// config value while being perfectly valid to the orchestrator.
func TestValidateOrchestration_TrustPolicy(t *testing.T) {
	t.Parallel()

	valid := append([]string{""}, OrchestrationTrustPolicies...)
	for _, policy := range valid {
		t.Run("valid/"+policy, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.validateOrchestration(&OrchestrationConfig{TrustPolicy: policy})
			if len(v.errors) != 0 {
				t.Fatalf("trust_policy %q: expected no validation errors, got %v", policy, v.errors)
			}
		})
	}

	for _, policy := range []string{"attest", "PINNED", "none", "opt in", "measured "} {
		t.Run("invalid/"+policy, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.validateOrchestration(&OrchestrationConfig{TrustPolicy: policy})
			if len(v.errors) == 0 {
				t.Fatalf("trust_policy %q: expected a validation error, got none", policy)
			}
			if !strings.Contains(v.errors[0].Field, "orchestration.trust_policy") {
				t.Fatalf("expected the error on orchestration.trust_policy, got %v", v.errors)
			}
		})
	}
}

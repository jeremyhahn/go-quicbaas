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

package fleet

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestLoadConfigFile_OverridesOnlyWhatItStates is the layering contract
// `fleet serve --config` relies on: the file replaces the keys it names and
// leaves every other seeded default alone.
func TestLoadConfigFile_OverridesOnlyWhatItStates(t *testing.T) {
	path := writeConfigFile(t, "tenant_id: acme\nmax_concurrent_jobs: 4\nsandbox_policy: preferred\nheartbeat_interval: 15s\n")
	cfg := &Config{
		BindAddr:      "127.0.0.1:9091",
		TenantID:      "seeded",
		SandboxPolicy: orchestration.SandboxPolicyRequired,
	}
	if err := LoadConfigFile(path, cfg); err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfg.TenantID != "acme" || cfg.MaxConcurrentJobs != 4 {
		t.Fatalf("file values not applied: %+v", cfg)
	}
	if cfg.SandboxPolicy != orchestration.SandboxPolicyPreferred {
		t.Fatalf("sandbox_policy = %v, want preferred", cfg.SandboxPolicy)
	}
	if cfg.HeartbeatInterval != 15*time.Second {
		t.Fatalf("heartbeat_interval = %v, want 15s", cfg.HeartbeatInterval)
	}
	if cfg.BindAddr != "127.0.0.1:9091" {
		t.Fatalf("seeded bind_addr overwritten: %q", cfg.BindAddr)
	}
}

// TestLoadConfigFile_RefusesUnknownKey proves a misspelt setting fails the
// load instead of being dropped while the daemon runs on the default.
func TestLoadConfigFile_RefusesUnknownKey(t *testing.T) {
	path := writeConfigFile(t, "max_concurent_jobs: 4\n")
	err := LoadConfigFile(path, &Config{})
	var fileErr *ConfigFileError
	if !errors.As(err, &fileErr) {
		t.Fatalf("expected *ConfigFileError, got %v", err)
	}
	if fileErr.Path != path || !strings.Contains(err.Error(), "max_concurent_jobs") {
		t.Fatalf("error does not name the file and key: %v", err)
	}
}

func TestLoadConfigFile_MissingFile(t *testing.T) {
	err := LoadConfigFile(filepath.Join(t.TempDir(), "absent.yaml"), &Config{})
	var fileErr *ConfigFileError
	if !errors.As(err, &fileErr) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected a ConfigFileError wrapping ErrNotExist, got %v", err)
	}
}

func TestLoadConfigFile_NilTarget(t *testing.T) {
	if err := LoadConfigFile("unused", nil); !errors.Is(err, ErrConfigNil) {
		t.Fatalf("expected ErrConfigNil, got %v", err)
	}
	if err := DecodeConfig(nil, nil); !errors.Is(err, ErrConfigNil) {
		t.Fatalf("expected ErrConfigNil, got %v", err)
	}
	if _, err := MarshalConfig(nil); !errors.Is(err, ErrConfigNil) {
		t.Fatalf("expected ErrConfigNil, got %v", err)
	}
}

func TestDecodeConfig_EmptyDocumentIsNoChange(t *testing.T) {
	cfg := &Config{TenantID: "kept"}
	if err := DecodeConfig([]byte(""), cfg); err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.TenantID != "kept" {
		t.Fatalf("empty document changed the config: %+v", cfg)
	}
}

// TestMarshalConfig_RoundTrips proves what a deploy renders is exactly what
// the daemon reads back.
func TestMarshalConfig_RoundTrips(t *testing.T) {
	want := &Config{
		ServerAddr:        "https://quicbaas.example.com:8443",
		TenantID:          "acme",
		BindAddr:          "0.0.0.0:9091",
		Backends:          []BackendKind{BackendACP},
		SandboxPolicy:     orchestration.SandboxPolicyRequired,
		ImageMap:          map[string]string{"claude-code": "quicbaas/claude-code:v1"},
		WorkDir:           "/srv/agent/work",
		MaxConcurrentJobs: 3,
		ShutdownGrace:     time.Minute,
		AllowedMountRoots: []string{"/srv/sockets"},
	}
	out, err := MarshalConfig(want)
	if err != nil {
		t.Fatalf("MarshalConfig: %v", err)
	}
	if !strings.Contains(string(out), "sandbox_policy: required") || !strings.Contains(string(out), "shutdown_grace: 1m0s") {
		t.Fatalf("rendered config is not human-readable:\n%s", out)
	}
	got := &Config{}
	if err := DecodeConfig(out, got); err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("round trip mismatch:\nwant %+v\ngot  %+v", want, got)
	}
}

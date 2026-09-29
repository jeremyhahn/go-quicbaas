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

package deploy

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// updateGolden rewrites the golden files from the current renderers:
//
//	go test ./pkg/fleet/deploy/ -run Golden -update
var updateGolden = flag.Bool("update", false, "rewrite golden files")

const fixtureSpec = "testdata/node-spec.yaml"

func loadFixtureSpec(t *testing.T) *Spec {
	t.Helper()
	spec := DefaultSpec()
	if err := LoadSpecFile(fixtureSpec, spec); err != nil {
		t.Fatalf("LoadSpecFile: %v", err)
	}
	return spec
}

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	golden := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.WriteFile(golden, got, 0o600); err != nil {
			t.Fatalf("update %s: %v", golden, err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read %s: %v (run with -update to create it)", golden, err)
	}
	if string(want) != string(got) {
		t.Fatalf("%s drifted (run with -update if the change is intended):\n--- want\n%s\n--- got\n%s", golden, want, got)
	}
}

// TestGolden_AgentConfig pins the daemon config deploy renders from a node
// spec: every agent-section setting carried through, every deploy-owned one
// filled in from the spec and the resolved layout.
func TestGolden_AgentConfig(t *testing.T) {
	spec := loadFixtureSpec(t)
	body, err := RenderNodeConfig(spec, spec.Layout("/home/agent"))
	if err != nil {
		t.Fatalf("RenderNodeConfig: %v", err)
	}
	assertGolden(t, "agent-config.golden.yaml", body)

	// What was rendered is what the daemon reads back, and it would start.
	got := &fleet.Config{}
	if err := fleet.DecodeConfig(body, got); err != nil {
		t.Fatalf("rendered config does not decode: %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("rendered config would not start: %v", err)
	}
	if got.MaxConcurrentJobs != 4 || got.SandboxPolicy != orchestration.SandboxPolicyRequired {
		t.Fatalf("capacity or sandbox policy lost in rendering: %+v", got)
	}
}

// TestGolden_Unit pins the systemd --user unit deploy installs.
func TestGolden_Unit(t *testing.T) {
	assertGolden(t, "unit.golden", []byte(RenderUnit(loadFixtureSpec(t))))
}

// TestGolden_StartScript pins the nohup fallback launcher.
func TestGolden_StartScript(t *testing.T) {
	assertGolden(t, "start-script.golden", []byte(RenderStartScript(loadFixtureSpec(t))))
}

func TestLoadSpecFile_ReadsEverySection(t *testing.T) {
	spec := loadFixtureSpec(t)
	if spec.Host != "agent-01.internal" || spec.SSHKeyName != "fleet-deploy" || spec.WaitOnline != 2*time.Minute {
		t.Fatalf("top-level fields not read: %+v", spec)
	}
	if spec.ProjectsRoot != "/srv/quicbaas/projects" || len(spec.WorkerImages) != 1 {
		t.Fatalf("projects_root / worker_images not read: %+v", spec)
	}
	if spec.Node.MaxConcurrentJobs != 4 || spec.Node.ImageMap["codex"] == "" {
		t.Fatalf("agent section not read: %+v", spec.Node)
	}
	// The fixture says nothing about sandboxing, so the seeded default stands.
	if spec.Node.SandboxPolicy != orchestration.SandboxPolicyRequired {
		t.Fatalf("sandbox policy = %v, want the required default", spec.Node.SandboxPolicy)
	}
	if err := spec.ValidateDaemonConfig(); err != nil {
		t.Fatalf("fixture does not validate: %v", err)
	}
}

func writeSpecFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return path
}

func TestLoadSpecFile_Refusals(t *testing.T) {
	cases := map[string]string{
		"unknown top-level key":  "hots: agent-01\n",
		"unknown agent key":      "agent:\n  max_concurent_jobs: 2\n",
		"password in a spec":     "ssh_password: hunter2\n",
		"skip_preflight is flag": "skip_preflight: true\n",
		"not yaml":               "host: [unterminated\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			err := LoadSpecFile(writeSpecFile(t, body), DefaultSpec())
			var fileErr *SpecFileError
			if !errors.As(err, &fileErr) {
				t.Fatalf("expected *SpecFileError, got %v", err)
			}
		})
	}
}

func TestLoadSpecFile_MissingAndNil(t *testing.T) {
	if err := LoadSpecFile(filepath.Join(t.TempDir(), "absent.yaml"), DefaultSpec()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected ErrNotExist, got %v", err)
	}
	var invalid *InvalidSpecError
	if err := LoadSpecFile(fixtureSpec, nil); !errors.As(err, &invalid) {
		t.Fatalf("expected *InvalidSpecError for nil target, got %v", err)
	}
}

func validDaemonSpec() *Spec {
	spec := DefaultSpec()
	spec.Host = "agent-01"
	spec.Server = "https://quicbaas.example.com:8443"
	spec.Tenant = "acme"
	spec.NodeBind = "0.0.0.0:9091"
	spec.Backends = []string{"acp"}
	spec.Node.MaxConcurrentJobs = 2
	return spec
}

// TestValidateDaemonConfig_RequiresCapacity is the rule that ends
// "undeclared" fleet capacity: a node spec that does not say how many jobs
// the host can run is refused before anything is installed.
func TestValidateDaemonConfig_RequiresCapacity(t *testing.T) {
	for _, jobs := range []int{0, -1} {
		spec := validDaemonSpec()
		spec.Node.MaxConcurrentJobs = jobs
		var undeclared *CapacityUndeclaredError
		err := spec.ValidateDaemonConfig()
		if !errors.As(err, &undeclared) {
			t.Fatalf("max_concurrent_jobs=%d: expected *CapacityUndeclaredError, got %v", jobs, err)
		}
		if !strings.Contains(err.Error(), "max_concurrent_jobs") {
			t.Fatalf("error does not name the fix: %v", err)
		}
	}
	if err := validDaemonSpec().ValidateDaemonConfig(); err != nil {
		t.Fatalf("valid spec refused: %v", err)
	}
}

// TestValidateDaemonConfig_RefusesDeployOwnedKeys: a setting the deploy fills
// in may not also be written in the node section.
func TestValidateDaemonConfig_RefusesDeployOwnedKeys(t *testing.T) {
	mutations := map[string]func(*fleet.Config){
		"node.server_addr":    func(c *fleet.Config) { c.ServerAddr = "https://other:8443" },
		"node.tenant_id":      func(c *fleet.Config) { c.TenantID = "other" },
		"node.bind_addr":      func(c *fleet.Config) { c.BindAddr = "127.0.0.1:1" },
		"node.backends":       func(c *fleet.Config) { c.Backends = []fleet.BackendKind{fleet.BackendACP} },
		"node.work_dir":       func(c *fleet.Config) { c.WorkDir = "/srv/w" },
		"node.projects_root":  func(c *fleet.Config) { c.ProjectsRoot = "/srv/p" },
		"node.node_cert_file": func(c *fleet.Config) { c.NodeCertFile = "/c" },
		"node.node_key_file":  func(c *fleet.Config) { c.NodeKeyFile = "/k" },
		"node.server_ca_file": func(c *fleet.Config) { c.ServerCAFile = "/ca" },
		"node.mcp_endpoint":   func(c *fleet.Config) { c.MCPEndpoint = "https://x/mcp" },
	}
	for field, mutate := range mutations {
		spec := validDaemonSpec()
		mutate(&spec.Node)
		var owned *DeployOwnedFieldError
		if err := spec.ValidateDaemonConfig(); !errors.As(err, &owned) || owned.Field != field {
			t.Fatalf("%s: expected DeployOwnedFieldError for it, got %v", field, err)
		}
	}
}

// TestValidateDaemonConfig_RefusesConfigTheDaemonWould: a config the daemon
// would refuse at start-up is refused at deploy time instead.
func TestValidateDaemonConfig_RefusesConfigTheDaemonWould(t *testing.T) {
	spec := validDaemonSpec()
	spec.Node.AllowedMountRoots = []string{"relative/root"}
	var invalid *AgentConfigInvalidError
	if err := spec.ValidateDaemonConfig(); !errors.As(err, &invalid) || !errors.Is(err, fleet.ErrConfigMountRootNotAbsolute) {
		t.Fatalf("expected AgentConfigInvalidError wrapping the mount-root error, got %v", err)
	}
	if _, err := RenderNodeConfig(spec, spec.Layout("/home/u")); !errors.As(err, &invalid) {
		t.Fatalf("RenderNodeConfig rendered a config the daemon refuses: %v", err)
	}
	var nilSpec *Spec
	if err := nilSpec.ValidateDaemonConfig(); err == nil {
		t.Fatal("nil spec validated")
	}
}

func TestLayout_ResolvesUnderHome(t *testing.T) {
	spec := &Spec{InstallDir: "/opt/bin", DataDir: "state", ProjectsRoot: "work-trees"}
	l := spec.Layout("/home/u")
	checks := map[string]string{
		l.Binary:       "/home/u/opt/bin/quicbaas",
		l.ConfigFile:   "/home/u/opt/bin/agent.yaml",
		l.StartScript:  "/home/u/opt/bin/start-quicbaas-agent.sh",
		l.WorkDir:      "/home/u/state/work",
		l.ProjectsRoot: "/home/u/work-trees",
		l.CertFile:     "/home/u/state/agent.crt",
		l.PIDFile:      "/home/u/state/agent.pid",
		l.UnitFile:     "/home/u/.config/systemd/user/quicbaas-agent.service",
	}
	for got, want := range checks {
		if got != want {
			t.Fatalf("layout path %q, want %q", got, want)
		}
	}
	if got := (&Spec{ProjectsRoot: "/srv/p"}).Layout("/home/u").ProjectsRoot; got != "/srv/p" {
		t.Fatalf("absolute projects root moved: %q", got)
	}
	if got := (&Spec{}).Layout("/home/u").ProjectsRoot; got != "/home/u/.quicbaas/agent/work/projects" {
		t.Fatalf("default projects root = %q", got)
	}
	if got := (&Spec{}).Layout("").DataDir; got != "$HOME/.quicbaas/agent" {
		t.Fatalf("empty home should yield $HOME paths, got %q", got)
	}
}

// TestCommittedFleetSpecsLoad: every node spec committed under
// deployments/fleet loads, declares its capacity and renders a config the
// daemon would start with. A spec that drifts from the schema fails here,
// not on the node.
func TestCommittedFleetSpecsLoad(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "deployments", "fleet", "*.yaml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no committed node specs found: %v", err)
	}
	for _, path := range paths {
		spec := DefaultSpec()
		if err := LoadSpecFile(path, spec); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := spec.ValidateDaemonConfig(); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if _, err := RenderNodeConfig(spec, spec.Layout("/home/agent")); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
}

// gpuHostSpec is the shape of a GPU node that serves the local review model and
// the NPU triage model and must never be handed hosted-model work.
const gpuHostSpec = `host: gpu-host.internal
ssh_user: agent
ssh_key_name: fleet-deploy
server: https://quicbaas.example.com:8443
tenant: acme
node_bind: 0.0.0.0:9091
backends: [opencode, openvino, ollama]
binary: bin/quicbaas-static
systemd: true
node:
  max_concurrent_jobs: 1
  sandbox_policy: required
  container_runtime: docker
  ollama_base_url: http://127.0.0.1:11434
  openvino_base_url: http://127.0.0.1:9000
  openvino_model: qwen3-8b
  openvino_context_tokens: 16384
`

// TestSpec_IndividualBackendsRenderWhatTheNodeServes: a spec naming opencode,
// openvino and ollama validates and renders a daemon that serves exactly
// those three — no claude, no codex.
func TestSpec_IndividualBackendsRenderWhatTheNodeServes(t *testing.T) {
	spec := DefaultSpec()
	if err := LoadSpecFile(writeSpecFile(t, gpuHostSpec), spec); err != nil {
		t.Fatalf("LoadSpecFile: %v", err)
	}
	if err := spec.ValidateDaemonConfig(); err != nil {
		t.Fatalf("ValidateDaemonConfig: %v", err)
	}
	served := spec.DaemonConfig(spec.Layout("/home/agent")).EffectiveBackends()
	want := []fleet.BackendKind{fleet.BackendOpencode, fleet.BackendOpenVINO, fleet.BackendOllama}
	if !slices.Equal(served, want) {
		t.Fatalf("served = %v, want %v", served, want)
	}
	rendered, err := RenderNodeConfig(spec, spec.Layout("/home/agent"))
	if err != nil {
		t.Fatalf("RenderAgentConfig: %v", err)
	}
	if !strings.Contains(string(rendered), "- opencode") || strings.Contains(string(rendered), "- acp") {
		t.Fatalf("rendered config does not name the individual backends:\n%s", rendered)
	}
}

// TestSpec_RefusesABackendTheDaemonWould: an unknown backend, or openvino with
// no server, is refused on the operator's machine rather than in a restart
// loop on the node.
func TestSpec_RefusesABackendTheDaemonWould(t *testing.T) {
	cases := map[string]struct {
		backends []string
		sentinel error
	}{
		"unknown backend":         {[]string{"opencode", "qwen"}, fleet.ErrConfigUnknownBackend},
		"openvino with no server": {[]string{"openvino"}, fleet.ErrConfigBackendEndpointRequired},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			spec := validDaemonSpec()
			spec.Backends = tc.backends
			var invalid *AgentConfigInvalidError
			err := spec.ValidateDaemonConfig()
			if !errors.As(err, &invalid) || !errors.Is(err, tc.sentinel) {
				t.Fatalf("ValidateDaemonConfig() = %v, want *AgentConfigInvalidError wrapping %v", err, tc.sentinel)
			}
		})
	}
}

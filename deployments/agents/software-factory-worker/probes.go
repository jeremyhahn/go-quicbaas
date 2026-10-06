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

package softwarefactoryworker

import (
	_ "embed"
	"strconv"
	"strings"
)

// =============================================================================
// The image's probes — what an image must do before anything is bound to it
// =============================================================================
//
// THIS IS THE ONE COPY. `make test-worker-image` runs these (through
// ./deployments/agents/software-factory-worker/probe) and the daemon's
// refresh gate runs these (pkg/sdlc/workerimage), so the image a person
// certifies by hand and the image the factory promotes by itself pass the
// same bar.
//
// EVERY PROBE RUNS `sh -lec`, AND THE -l IS LOAD-BEARING. The agent execs
// tools in this image as `sh -lc <command>`. A LOGIN shell sources
// /etc/profile, whose first line on this base image unconditionally overwrites
// PATH — discarding the Dockerfile's ENV PATH and with it the Go toolchain.
// Probes that used `sh -euc` asserted a PATH production never saw: the gate
// reported the toolchain present while `go` was unreachable to every real
// invocation, and five delivery jobs stalled on it before it was found.
//
// EVERY PROBE RUNS AS THE UID THE SPAWN WILL USE, with HOME=/tmp, because the
// failures worth catching (ssh-keygen's getpwuid, sudo's passwd lookup, a
// root-owned write through a bind mount) are properties of that uid.
//
// Expected versions reach a script through its ENVIRONMENT, never by being
// spliced into its text, so nothing a registry answered can become shell.

// Probe is one check an image must pass.
type Probe struct {
	// Name says what the probe proves, for the report.
	Name string

	// Script is run as `sh -lec <Script>` inside the image.
	Script string

	// NetworkNone runs the probe with no network: what it proves must hold in
	// a sandbox whose egress the operator has closed.
	NetworkNone bool

	// WorkspaceMount bind-mounts a fresh host directory at /workspace. The
	// probe then passes only if every file the script wrote there is owned, on
	// the host, by the uid it ran as.
	WorkspaceMount bool

	// Sandboxed runs the probe under the boundary production draws around a
	// worker (go-quicbaas#1040): every capability dropped, the policy's
	// add-backs, its pids limit and its ulimits — rendered by the same code
	// the spawn uses, so the probe cannot certify a boundary no worker runs
	// with.
	Sandboxed bool

	// PidsLimit, when non-zero, lowers a Sandboxed probe's process bound so a
	// fork probe can reach it without starting thousands of processes. The
	// capability flags stay the production ones.
	PidsLimit int64

	// Stdin is fed to the script's standard input.
	Stdin []byte
}

// ProbePidsLimit is the process bound the fork probe runs under: small, so it
// reaches the bound quickly. Production's is
// orchestration.DefaultSandboxPidsLimit.
const ProbePidsLimit = 64

// pidsLimitProbeScript is the fork probe, fed to python3 on stdin. See the
// file for why it is Python rather than a shell loop.
//
//go:embed pids-limit-probe.py
var pidsLimitProbeScript []byte

// The environment variables a probe reads its expectations from.
const (
	EnvExpectedClaudeCode = "EXPECTED_CLAUDE_CODE_VERSION"
	EnvExpectedCodex      = "EXPECTED_CODEX_VERSION"
	EnvExpectedGopls      = "EXPECTED_GOPLS_VERSION"
	EnvExpectedGolangci   = "EXPECTED_GOLANGCI_VERSION"
	EnvExpectedActionlint = "EXPECTED_ACTIONLINT_VERSION"
	EnvExpectedShellcheck = "EXPECTED_SHELLCHECK_VERSION"
	EnvExpectedOpencode   = "EXPECTED_OPENCODE_VERSION"
	EnvProbePidsLimit     = "PROBE_PIDS_LIMIT"

	// EnvExpectedMemlockKiB is the locked-memory limit, in KiB as `ulimit -l`
	// reports it, a Sandboxed probe must see. It is not set by ProbeEnv: this
	// package holds no policy, so the probe runner derives it from the policy
	// it launches the probe under (pkg/sdlc/workerimage).
	EnvExpectedMemlockKiB = "EXPECTED_MEMLOCK_KIB"
)

// ProbeEnv is the environment every probe of b runs with.
func ProbeEnv(b Build) map[string]string {
	return map[string]string{
		"HOME":                "/tmp",
		EnvExpectedClaudeCode: b.Agents.ClaudeCode,
		EnvExpectedCodex:      b.Agents.Codex,
		EnvExpectedGopls:      b.Pins.Gopls,
		// golangci-lint reports "version 2.13.2", without the tag's "v".
		EnvExpectedGolangci: strings.TrimPrefix(b.Pins.Golangci, "v"),
		// actionlint reports its tag ("v1.7.12"); shellcheck reports
		// "version: 0.11.0", without the tag's "v".
		EnvExpectedActionlint: b.Pins.Actionlint,
		EnvExpectedShellcheck: strings.TrimPrefix(b.Pins.Shellcheck, "v"),
		// opencode reports "1.18.32", without the tag's "v".
		EnvExpectedOpencode: strings.TrimPrefix(b.Pins.Opencode, "v"),
		EnvProbePidsLimit:   strconv.Itoa(ProbePidsLimit),
	}
}

// Probes are the checks, in the order they are run: cheapest and most
// fundamental first, so a broken image fails on the first thing that is wrong
// with it.
func Probes() []Probe {
	return []Probe{
		{Name: "toolchain, signing and every agent", Script: toolchainProbe},
		{Name: "devcache-env changes nothing when unconfigured", Script: devcacheEnvProbe, NetworkNone: true},
		{Name: "serena answers an MCP initialize", Script: serenaHandshakeProbe, NetworkNone: true},
		{Name: "serena resolves a Go symbol", Script: serenaGoProbe, NetworkNone: true},
		{Name: "lint and scan a Go module", Script: lintProbe, NetworkNone: true},
		{Name: "lint GitHub Actions workflows", Script: workflowLintProbe, NetworkNone: true},
		{Name: "full control of the container, under the production boundary", Script: fullControlProbe, Sandboxed: true},
		{Name: "a fork past the pids limit fails cleanly", Script: pidsLimitProbe, Sandboxed: true,
			PidsLimit: ProbePidsLimit, Stdin: pidsLimitProbeScript},
		{Name: "locked memory has the CI runners' memlock limit", Script: memlockProbe, Sandboxed: true, NetworkNone: true},
		{Name: "bind-mounted writes stay owned by the spawning uid", Script: bindMountProbe, WorkspaceMount: true},
	}
}

// toolchainProbe proves the checkout, build and publish tools are on the login
// PATH, the agent CLIs are the versions the labels claim, and a signed commit
// verifies as this uid.
//
// The agent versions are asserted, not merely printed: the labels are what a
// refresh decides by, so an image whose labels lie about its CLIs would be
// refreshed never, or forever.
const toolchainProbe = `git --version; ssh -V; go version; make --version | head -1; rsync --version | head -1
grep --version | head -1; cryptsetup --version
pkg-config --exists webkit2gtk-4.1 || { echo "webkit2gtk-4.1 dev headers missing: make build cannot link the client"; exit 1; }
case "$(npm config get omit)" in dev*) echo "npm omits devDependencies: NODE_ENV must not be production"; exit 1;; esac
claude --version | grep -qF "$EXPECTED_CLAUDE_CODE_VERSION" || { echo "claude is not $EXPECTED_CLAUDE_CODE_VERSION: $(claude --version)"; exit 1; }
codex --version | grep -qF "$EXPECTED_CODEX_VERSION" || { echo "codex is not $EXPECTED_CODEX_VERSION: $(codex --version)"; exit 1; }
opencode --version | grep -qxF "$EXPECTED_OPENCODE_VERSION" || { echo "opencode is not the pinned $EXPECTED_OPENCODE_VERSION: $(opencode --version)"; exit 1; }
ssh-keygen -t ed25519 -N "" -f /tmp/signing-key -q
git init -q /tmp/probe; cd /tmp/probe
git -c user.name=worker -c user.email=worker@invalid -c gpg.format=ssh -c user.signingkey=/tmp/signing-key -c commit.gpgsign=true commit -q --allow-empty -m "the image can sign"
git config gpg.ssh.allowedSignersFile /tmp/allowed
printf "worker@invalid %s" "$(cat /tmp/signing-key.pub)" > /tmp/allowed
git verify-commit HEAD`

// devcacheEnvProbe proves the image carries an executable devcache-env
// (go-quicbaas#1159) and that, unconfigured, it is a no-op: with no
// DEVCACHE_* variable set -- the state of every build that does not pass the
// cache build arguments -- it must set neither GOPROXY nor
// npm_config_registry, so a worker spawned from this image reaches the public
// proxy and the public npm registry exactly as it did before the caches
// existed.
const devcacheEnvProbe = `test -x /usr/local/bin/devcache-env || { echo "devcache-env is missing or not executable"; exit 1; }
unset DEVCACHE_GOPROXY DEVCACHE_GOPRIVATE DEVCACHE_NPM_REGISTRY DEVCACHE_CONFIG
out="$(devcache-env)"
case "$out" in *GOPROXY=*) echo "devcache-env set GOPROXY with no DEVCACHE_GOPROXY configured: $out"; exit 1;; esac
case "$out" in *npm_config_registry=*) echo "devcache-env set npm_config_registry with no DEVCACHE_NPM_REGISTRY configured: $out"; exit 1;; esac`

// serenaHandshakeProbe proves serena STARTS, not merely that it was installed:
// psutil is compiled from source here (no musl wheel), so the failure worth
// catching is a native extension that builds and then will not import — which
// a version string does not exercise and an initialize request does.
//
// The project root is a directory HOLDING repositories, because that is what a
// real spawn hands it: the project and its context repositories side by side.
const serenaHandshakeProbe = `serena --version; gopls version | head -1
gopls version | grep -qF "$EXPECTED_GOPLS_VERSION" || { echo "gopls is not the pinned $EXPECTED_GOPLS_VERSION; serena cannot read Go at the version CI lints"; exit 1; }
mkdir -p /tmp/wsroot/a-repo /tmp/wsroot/a-context-repo
printf "%s\n" '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}' | timeout 120 serena start-mcp-server --transport stdio --context claude-code --project /tmp/wsroot 2>/dev/null | head -c 2000 | grep -q '"name":"Serena"' || { echo "serena did not answer an MCP initialize request"; exit 1; }`

// serenaGoProbe proves serena can resolve a GO language server, not merely
// answer a handshake. The handshake passed for weeks while serena could not
// read Go at all: solidlsp cannot detect musl, computed the platform id
// "linux--x64", and raised before gopls was reached, so every review lost
// symbol lookup and returned a verdict that decided nothing.
const serenaGoProbe = `mkdir -p /tmp/goprobe; cd /tmp/goprobe
printf "module example.com/probe\n\ngo 1.26\n" > go.mod
printf "package probe\n\nfunc ProbeSymbol() string { return \"found\" }\n" > probe.go
python3 -c 'import sys; sys.path.insert(0, "/opt/uv/tools/serena-agent/lib/python3.12/site-packages")
from solidlsp.ls_config import LanguageServerConfig, LanguageServerId
from solidlsp import SolidLanguageServer
from solidlsp.settings import SolidLSPSettings
ls = SolidLanguageServer.create(LanguageServerConfig(ls_id=LanguageServerId.GO), "/tmp/goprobe", solidlsp_settings=SolidLSPSettings())
print("go language server:", type(ls).__name__)' || { echo "serena cannot resolve Go in this image; the reviewer would lose symbol lookup"; exit 1; }`

// lintProbe is the verification half (go-quicbaas#772). A worker that cannot
// run `make lint` fails every review round on a clean tree, so the tools are
// checked the way a project's Makefile finds them — at $(go env GOPATH)/bin,
// not merely on the PATH — and at the pinned version, because a worker that
// lints with a different version than CI produces verdicts a merge then
// contradicts. And the linter must actually LINT: a package with an unused
// function has to fail.
const lintProbe = `golangci-lint --version; gosec -version; govulncheck -version
test -x "$(go env GOPATH)/bin/golangci-lint" || { echo "golangci-lint is not where a project Makefile looks for it"; exit 1; }
golangci-lint --version | grep -qF "version $EXPECTED_GOLANGCI_VERSION " || { echo "golangci-lint is not the pinned $EXPECTED_GOLANGCI_VERSION"; exit 1; }
mkdir -p /tmp/lintprobe; cd /tmp/lintprobe
printf "%s\n" "module lintprobe" "" "go 1.26" > go.mod
printf "%s\n" "version: \"2\"" "linters:" "  enable:" "    - misspell" "    - gocritic" > .golangci.yml
printf "%s\n" "// Package lintprobe proves this image can lint a Go module." "package lintprobe" "" "// Sum returns a plus b." "func Sum(a, b int) int { return a + b }" > sum.go
golangci-lint run ./...
printf "%s\n" "package lintprobe" "" "func unusedHelper() int { return 0 }" > unused.go
if golangci-lint run ./... >/dev/null 2>&1; then echo "golangci-lint passed a package with an unused function -- it is not linting"; exit 1; fi`

// workflowLintProbe (go-quicbaas#1039). A project whose `make lint` runs
// actionlint fails closed when shellcheck is missing, because actionlint would
// otherwise skip every SC* rule and still exit 0. So the probe proves the PAIR
// works, not that two binaries exist: a workflow whose run step word-splits an
// unquoted command substitution must be REPORTED as SC2046, which only happens
// when actionlint actually reaches shellcheck. Both at the pinned versions.
const workflowLintProbe = `actionlint --version; shellcheck --version
actionlint --version | head -1 | grep -qxF "$EXPECTED_ACTIONLINT_VERSION" || { echo "actionlint is not the pinned $EXPECTED_ACTIONLINT_VERSION"; exit 1; }
shellcheck --version | grep -qxF "version: $EXPECTED_SHELLCHECK_VERSION" || { echo "shellcheck is not the pinned $EXPECTED_SHELLCHECK_VERSION"; exit 1; }
mkdir -p /tmp/wfprobe/.github/workflows; cd /tmp/wfprobe
printf "%s\n" "on: push" "jobs:" "  probe:" "    runs-on: ubuntu-latest" "    steps:" "      - run: echo ok" > .github/workflows/ok.yml
actionlint .github/workflows/ok.yml
printf "%s\n" "on: push" "jobs:" "  probe:" "    runs-on: ubuntu-latest" "    steps:" '      - run: ls $(pwd)' > .github/workflows/bad.yml
if out="$(actionlint .github/workflows/bad.yml 2>&1)"; then echo "actionlint passed a workflow with an SC2046 word-split -- shellcheck is not being reached"; exit 1; fi
echo "$out" | grep -q SC2046 || { echo "actionlint failed without reporting SC2046: $out"; exit 1; }`

// fullControlProbe: the container is the boundary, so the worker has full
// control inside it (go-quicbaas#927). A worker that cannot install a package
// cannot finish a task whose toolchain the image does not carry. Proved as the
// spawn's uid, because sudo resolves the invoking user through getpwuid and
// refuses outright for a uid with no /etc/passwd entry.
//
// AND PROVED UNDER THE BOUNDARY PRODUCTION DRAWS AROUND IT (go-quicbaas#1040):
// the probe runs Sandboxed, so a missing add-back fails here as the command it
// breaks — sudo's setresuid, apk's chown — rather than as a stalled delivery.
// Root must hold exactly CHOWN, DAC_OVERRIDE, FOWNER, SETGID and SETUID (0xcb):
// no NET_RAW, no MKNOD, and no device node can be created.
const fullControlProbe = `sudo -n true
test "$(sudo -n id -u)" = "0" || { echo "sudo did not reach root"; exit 1; }
sudo -n apk add --no-cache jq; jq --version
sudo -n sh -c "echo arbitrary > /etc/quicbaas-full-control-probe"
test -f /etc/quicbaas-full-control-probe
eff="$(sudo -n awk '/^CapEff:/ {print $2}' /proc/self/status)"
echo "root CapEff: $eff"
test "$(( 0x$eff & (1 << 13) ))" -eq 0 || { echo "root in the worker holds CAP_NET_RAW"; exit 1; }
test "$(( 0x$eff & (1 << 27) ))" -eq 0 || { echo "root in the worker holds CAP_MKNOD"; exit 1; }
test "$(( 0x$eff ))" -eq "$(( 0xcb ))" || { echo "root CapEff $eff is not exactly CHOWN,DAC_OVERRIDE,FOWNER,SETGID,SETUID (0xcb)"; exit 1; }
if sudo -n mknod /tmp/quicbaas-mknod-probe c 1 3 2>/dev/null; then echo "root in the worker created a device node"; exit 1; fi`

// pidsLimitProbe: the pids bound holds, and fails cleanly. A fork past the
// limit must be refused inside the container (EAGAIN), not take the host's
// pid space — and the container must run a command again once those
// processes are gone. The script arrives on stdin (pids-limit-probe.py).
const pidsLimitProbe = `python3 - "$PROBE_PIDS_LIMIT"`

// memlockProbe: locked memory has the limit CI gives it (go-quicbaas#1038).
// Docker's default RLIMIT_MEMLOCK is 8 MiB, which a suite holding its secrets
// in mlocked memory exhausts, so it failed in the worker and passed in CI —
// and the worker cannot raise the hard limit itself, because that takes
// CAP_SYS_RESOURCE, which the boundary drops. The limit is asserted soft and
// hard, and then EXERCISED: 64 MiB, eight times Docker's default, is locked
// for real, so the probe proves what a crypto suite needs rather than what a
// shell builtin prints.
const memlockProbe = `echo "memlock soft $(ulimit -S -l) KiB, hard $(ulimit -H -l) KiB"
test "$(ulimit -S -l)" = "$EXPECTED_MEMLOCK_KIB" || { echo "the soft memlock limit is $(ulimit -S -l) KiB, not the $EXPECTED_MEMLOCK_KIB KiB the CI runners give"; exit 1; }
test "$(ulimit -H -l)" = "$EXPECTED_MEMLOCK_KIB" || { echo "the hard memlock limit is $(ulimit -H -l) KiB, not $EXPECTED_MEMLOCK_KIB KiB"; exit 1; }
python3 -c 'import ctypes, mmap, os, sys
size = 64 << 20
region = mmap.mmap(-1, size)
libc = ctypes.CDLL(None, use_errno=True)
address = ctypes.addressof(ctypes.c_char.from_buffer(region))
if libc.mlock(ctypes.c_void_p(address), ctypes.c_size_t(size)) != 0:
    sys.exit("mlock of 64 MiB failed: " + os.strerror(ctypes.get_errno()))
print("mlocked 64 MiB")'`

// bindMountProbe: the cost of that privilege is NOT paid on the host. The
// spawn runs as the daemon's uid rather than root precisely so a write through
// a bind mount lands owned by the daemon; a root-owned tree in the workspace
// root is one the daemon can neither clean up nor re-clone into. The ownership
// itself is checked on the host by the probe runner.
const bindMountProbe = `echo work > /workspace/written-by-the-agent; sudo -n true`

// ProbeWorkspaceFile is the file bindMountProbe writes, whose host-side owner
// the runner checks.
const ProbeWorkspaceFile = "written-by-the-agent"

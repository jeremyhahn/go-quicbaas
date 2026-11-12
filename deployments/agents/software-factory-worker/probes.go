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
	"archive/tar"
	"bytes"
	"embed"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"
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

// kotlinLspProbeScript starts the Kotlin language server and completes an LSP
// initialize over stdio, fed to python3 on stdin. Python bounds every read by
// one deadline, which a shell pipe into the server cannot.
//
//go:embed kotlin-lsp-probe.py
var kotlinLspProbeScript []byte

// The environment variables a probe reads its expectations from.
const (
	EnvExpectedClaudeCode      = "EXPECTED_CLAUDE_CODE_VERSION"
	EnvExpectedCodex           = "EXPECTED_CODEX_VERSION"
	EnvExpectedGemini          = "EXPECTED_GEMINI_CLI_VERSION"
	EnvExpectedGopls           = "EXPECTED_GOPLS_VERSION"
	EnvExpectedGolangci        = "EXPECTED_GOLANGCI_VERSION"
	EnvExpectedActionlint      = "EXPECTED_ACTIONLINT_VERSION"
	EnvExpectedShellcheck      = "EXPECTED_SHELLCHECK_VERSION"
	EnvExpectedOpencode        = "EXPECTED_OPENCODE_VERSION"
	EnvExpectedProtoc          = "EXPECTED_PROTOC_VERSION"
	EnvExpectedProtocGenGo     = "EXPECTED_PROTOC_GEN_GO_VERSION"
	EnvExpectedProtocGenGoGRPC = "EXPECTED_PROTOC_GEN_GO_GRPC_VERSION"
	EnvProbePidsLimit          = "PROBE_PIDS_LIMIT"
	EnvExpectedOpenJDK         = "EXPECTED_OPENJDK_MAJOR"

	// EnvExpectedNpmBundledFloors is the space-separated name@version floors
	// npm's bundled packages must meet: NPM_BUNDLED_DEPENDENCY_FLOORS without
	// the integrities, which only the build needs.
	EnvExpectedNpmBundledFloors = "EXPECTED_NPM_BUNDLED_DEPENDENCY_FLOORS"

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
		EnvExpectedGemini:     b.Agents.Gemini,
		EnvExpectedGopls:      b.Pins.Gopls,
		// golangci-lint reports "version 2.13.2", without the tag's "v".
		EnvExpectedGolangci: strings.TrimPrefix(b.Pins.Golangci, "v"),
		// actionlint reports its tag ("v1.7.12"); shellcheck reports
		// "version: 0.11.0", without the tag's "v".
		EnvExpectedActionlint: b.Pins.Actionlint,
		EnvExpectedShellcheck: strings.TrimPrefix(b.Pins.Shellcheck, "v"),
		// opencode reports "1.18.32", without the tag's "v".
		EnvExpectedOpencode: strings.TrimPrefix(b.Pins.Opencode, "v"),
		// protoc reports "libprotoc <version>", e.g. "libprotoc 36.2".
		// protoc-gen-go reports its tag ("v1.36.12"); protoc-gen-go-grpc
		// reports its version without the tag's "v".
		EnvExpectedProtoc:          b.Pins.Protoc,
		EnvExpectedProtocGenGo:     b.Pins.ProtocGenGo,
		EnvExpectedProtocGenGoGRPC: strings.TrimPrefix(b.Pins.ProtocGenGoGRPC, "v"),
		EnvProbePidsLimit:          strconv.Itoa(ProbePidsLimit),
		// java -version reports `openjdk version "21.0.8" ...`: the major
		// is checked, as Debian's archive moves the patch release.
		EnvExpectedOpenJDK: b.Pins.OpenJDK,

		EnvExpectedNpmBundledFloors: npmBundledFloorSpecs(b.Pins.NpmBundledDependencyFloors),
	}
}

// npmBundledFloorSpecs drops the integrity from each name@version=integrity
// floor, leaving the name@version pairs a probe compares installs with.
func npmBundledFloorSpecs(floors string) string {
	specs := strings.Fields(floors)
	for i, floor := range specs {
		specs[i], _, _ = strings.Cut(floor, "=")
	}
	return strings.Join(specs, " ")
}

// Probes are the checks, in the order they are run: cheapest and most
// fundamental first, so a broken image fails on the first thing that is wrong
// with it.
func Probes() []Probe {
	return []Probe{
		{Name: "toolchain, signing and every agent", Script: toolchainProbe},
		{Name: "devcache-env changes nothing when unconfigured", Script: devcacheEnvProbe, NetworkNone: true},
		{Name: "serena answers an MCP initialize", Script: serenaHandshakeProbe, NetworkNone: true},
		{Name: "serena reads every baked-in language by symbol, offline from an empty HOME",
			Script: serenaLanguageServersProbe, Sandboxed: true, NetworkNone: true, Stdin: serenaFixtureArchive},
		{Name: "the Kotlin language server starts offline and answers an LSP initialize",
			Script: kotlinLspProbe, Sandboxed: true, NetworkNone: true, Stdin: kotlinLspProbeScript},
		{Name: "lint and scan a Go module", Script: lintProbe, NetworkNone: true},
		{Name: "lint GitHub Actions workflows", Script: workflowLintProbe, NetworkNone: true},
		{Name: "protoc generates Go and gRPC code for a proto importing the well-known types", Script: protocProbe, NetworkNone: true},
		{Name: "cgo compiles against the OpenSSL and libusb headers", Script: cgoHeadersProbe, NetworkNone: true},
		{Name: "npm's bundled dependencies meet their floors, and npm still globs", Script: npmBundledFloorsProbe, NetworkNone: true},
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
node -e "process.exit(typeof localStorage === 'undefined' ? 0 : 1)" || { echo "node defines a global localStorage that shadows jsdom's: NODE_OPTIONS must carry --no-experimental-webstorage"; exit 1; }
claude --version | grep -qF "$EXPECTED_CLAUDE_CODE_VERSION" || { echo "claude is not $EXPECTED_CLAUDE_CODE_VERSION: $(claude --version)"; exit 1; }
codex --version | grep -qF "$EXPECTED_CODEX_VERSION" || { echo "codex is not $EXPECTED_CODEX_VERSION: $(codex --version)"; exit 1; }
gemini --version | grep -qF "$EXPECTED_GEMINI_CLI_VERSION" || { echo "gemini is not $EXPECTED_GEMINI_CLI_VERSION: $(gemini --version)"; exit 1; }
opencode --version | grep -qxF "$EXPECTED_OPENCODE_VERSION" || { echo "opencode is not the pinned $EXPECTED_OPENCODE_VERSION: $(opencode --version)"; exit 1; }
java -version 2>&1 | head -1 | grep -qF "\"$EXPECTED_OPENJDK_MAJOR." || { echo "java is not OpenJDK $EXPECTED_OPENJDK_MAJOR: $(java -version 2>&1 | head -1)"; exit 1; }
command -v kotlin-lsp >/dev/null || { echo "kotlin-lsp is not on the PATH"; exit 1; }
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
// the failure worth catching is a native extension (psutil) that installs and
// then will not import — which a version string does not exercise and an
// initialize request does.
//
// The project root is a directory HOLDING repositories, because that is what a
// real spawn hands it: the project and its context repositories side by side.
const serenaHandshakeProbe = `serena --version; gopls version | head -1
gopls version | grep -qF "$EXPECTED_GOPLS_VERSION" || { echo "gopls is not the pinned $EXPECTED_GOPLS_VERSION; serena cannot read Go at the version CI lints"; exit 1; }
mkdir -p /tmp/wsroot/a-repo /tmp/wsroot/a-context-repo
printf "%s\n" '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}' | timeout 120 serena start-mcp-server --transport stdio --context claude-code --project /tmp/wsroot 2>/dev/null | head -c 2000 | grep -q '"name":"Serena"' || { echo "serena did not answer an MCP initialize request"; exit 1; }`

// serenaLanguageServersProbe proves every language server the image bakes in
// RETURNS A SYMBOL through serena, not merely that it is installed. The
// handshake once passed for weeks while serena could not read Go at all, so
// every review lost symbol lookup and returned a verdict that decided nothing.
//
// It runs the way a lane does: under the production boundary, with no network
// and a fresh, empty HOME per language — so a server serena would otherwise
// download into ~/.serena on first use fails here, and so does one the image's
// global settings (the ls_path entries in serena's config template) do not
// point at. The fixtures (testdata/serena: one module, one function, one
// caller per language) arrive on stdin as a tar stream. Every language is
// tried before the probe fails, so one run names every broken server.
const serenaLanguageServersProbe = `fixtures="$(mktemp -d)"
tar -xf - -C "$fixtures"
symbol_from() {
	lang="$1" file="$2" symbol="$3" home="$(mktemp -d)"
	project="$fixtures/$lang"
	if ! HOME="$home" serena project create "$project" --language "$lang" > "$home/create.log" 2>&1; then
		echo "serena could not create a $lang project:"; tail -n 20 "$home/create.log"; return 1
	fi
	if ! HOME="$home" timeout 300 serena project index-file "$project/$file" "$project" -v > "$home/index.log" 2>&1; then
		echo "serena could not index the $lang fixture $file:"; tail -n 20 "$home/index.log"; return 1
	fi
	if ! grep -qF -- "- $symbol at line" "$home/index.log"; then
		echo "the $lang language server did not return $symbol from $file:"; tail -n 20 "$home/index.log"; return 1
	fi
	if [ -n "$(find "$home" -path '*/language_servers/*' -type f | head -n 1)" ]; then
		echo "serena downloaded a $lang language server into the empty HOME instead of using the baked-in one"; return 1
	fi
	echo "$lang: $symbol"
}
failed=""
symbol_from go greeter.go Greet || failed="$failed go"
symbol_from python greeter.py greet || failed="$failed python"
symbol_from typescript greeter.ts greet || failed="$failed typescript"
symbol_from rust src/lib.rs greet || failed="$failed rust"
symbol_from cpp greeter.cpp greet || failed="$failed cpp"
symbol_from ruby greeter.rb self.greet || failed="$failed ruby"
symbol_from bash greeter.sh greet || failed="$failed bash"
symbol_from markdown greeting.md Greeting || failed="$failed markdown"
symbol_from yaml greeting.yaml greeting || failed="$failed yaml"
test -z "$failed" || { echo "serena read no symbol in:$failed"; exit 1; }`

// kotlinLspProbe: the Kotlin language server the toolchain probe finds on the
// PATH starts, offline and from an empty HOME, as the worker uid, and answers
// an LSP initialize with its capabilities before shutting down
// (go-quicbaas#1746). The script arrives on stdin (kotlin-lsp-probe.py).
const kotlinLspProbe = `python3 -`

// serenaFixtures are the fixture repositories serenaLanguageServersProbe
// reads, one per language.
//
//go:embed testdata/serena
var serenaFixtures embed.FS

// serenaFixtureArchive is serenaFixtures as the tar stream the probe reads on
// stdin. Built once: the fixtures are compiled in, so a failure to archive them
// is a defect in this package, not a runtime condition.
var serenaFixtureArchive = mustFixtureArchive(serenaFixtures, "testdata/serena")

// fixtureGoMod is the name the Go fixture's go.mod is stored under. go:embed
// refuses a directory holding a go.mod (it is another module), so the file is
// renamed back to go.mod as it is archived.
const fixtureGoMod = "go.mod.fixture"

// fixtureArchive tars the tree under root in fsys, with names relative to
// root, directories 0755, files 0644 and every timestamp the epoch, so the
// same fixtures always make the same bytes.
func fixtureArchive(fsys fs.FS, root string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	walkErr := fs.WalkDir(fsys, root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(name, root), "/")
		if rel == "" {
			return nil
		}
		if entry.IsDir() {
			return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: rel + "/", Mode: 0o755, ModTime: time.Unix(0, 0)})
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		if path.Base(rel) == fixtureGoMod {
			rel = path.Join(path.Dir(rel), "go.mod")
		}
		header := &tar.Header{Typeflag: tar.TypeReg, Name: rel, Mode: 0o644, Size: int64(len(data)), ModTime: time.Unix(0, 0)}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if walkErr != nil {
		return nil, &FixtureArchiveError{Root: root, Cause: walkErr}
	}
	if err := tw.Close(); err != nil {
		return nil, &FixtureArchiveError{Root: root, Cause: err}
	}
	return buf.Bytes(), nil
}

// mustFixtureArchive is fixtureArchive for fixtures compiled into the binary.
func mustFixtureArchive(fsys fs.FS, root string) []byte {
	archive, err := fixtureArchive(fsys, root)
	if err != nil {
		panic(err)
	}
	return archive
}

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

// protocProbe proves protoc and its two Go code-generator plugins are the
// pinned versions and that the TRIO works, not that three binaries exist: a
// proto importing a well-known type (google/protobuf/timestamp.proto, which
// ships in protoc's own include/ tree) and declaring a service must generate
// both a .pb.go and a _grpc.pb.go, offline.
const protocProbe = `protoc --version; protoc-gen-go --version; protoc-gen-go-grpc --version
protoc --version | grep -qF "$EXPECTED_PROTOC_VERSION" || { echo "protoc is not the pinned $EXPECTED_PROTOC_VERSION: $(protoc --version)"; exit 1; }
protoc-gen-go --version | grep -qF "$EXPECTED_PROTOC_GEN_GO_VERSION" || { echo "protoc-gen-go is not the pinned $EXPECTED_PROTOC_GEN_GO_VERSION: $(protoc-gen-go --version)"; exit 1; }
protoc-gen-go-grpc --version | grep -qF "$EXPECTED_PROTOC_GEN_GO_GRPC_VERSION" || { echo "protoc-gen-go-grpc is not the pinned $EXPECTED_PROTOC_GEN_GO_GRPC_VERSION: $(protoc-gen-go-grpc --version)"; exit 1; }
mkdir -p /tmp/protocprobe; cd /tmp/protocprobe
printf "%s\n" 'syntax = "proto3";' '' 'package protocprobe;' '' 'import "google/protobuf/timestamp.proto";' '' 'option go_package = "example.com/protocprobe;protocprobe";' '' 'message Ping { google.protobuf.Timestamp sent_at = 1; }' '' 'service Prober { rpc Probe(Ping) returns (Ping); }' > probe.proto
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative probe.proto
test -f probe.pb.go || { echo "protoc did not generate probe.pb.go"; exit 1; }
test -f probe_grpc.pb.go || { echo "protoc did not generate probe_grpc.pb.go"; exit 1; }`

// cgoHeadersProbe compiles and links a cgo program that includes the C headers
// portfolio dependencies include: <openssl/aes.h> (go-tpm-tools' TPM simulator,
// for go-xkms pkg/tpm2) and <libusb-1.0/libusb.h> (go-xkms xkey).
const cgoHeadersProbe = `d=$(mktemp -d); cd "$d"
printf "%s\n" 'module example.com/cgoheaders' '' 'go 1.21' > go.mod
printf "%s\n" 'package main' '' '// #cgo pkg-config: libcrypto libusb-1.0' '// #include <openssl/aes.h>' '// #include <libusb-1.0/libusb.h>' 'import "C"' '' 'func main() { println(C.AES_BLOCK_SIZE, C.libusb_get_version().major) }' > main.go
CGO_ENABLED=1 GOFLAGS=-mod=mod go run . || { echo "a cgo program including openssl/aes.h and libusb-1.0/libusb.h did not build"; exit 1; }`

// npmBundledFloorsProbe proves the build's replacement of npm's own
// brace-expansion and undici took (toolchain.mk's
// NPM_BUNDLED_DEPENDENCY_FLOORS): every copy of each floored package under
// npm's node_modules is at or above its floor, since the trivy gate reads
// every copy. Then npm itself runs on the replacements: undici loads, and
// `npm pack` expands a brace glob in "files" (minimatch, through
// brace-expansion) to exactly the files it names.
const npmBundledFloorsProbe = `modules="$(npm root -g)/npm/node_modules"
for floor in $EXPECTED_NPM_BUNDLED_DEPENDENCY_FLOORS; do
  name="${floor%@*}"; want="${floor##*@}"
  copies="$(find "$modules" -path "*/node_modules/$name/package.json")"
  [ -n "$copies" ] || { echo "npm bundles no $name"; exit 1; }
  for manifest in $copies; do
    have="$(node -p 'require(process.argv[1]).version' "$manifest")"
    [ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -n 1)" = "$want" ] || { echo "$manifest is $name $have, below the floor $want"; exit 1; }
  done
done
node -e 'require(process.argv[1])' "$modules/undici"
mkdir -p /tmp/npmprobe; cd /tmp/npmprobe
printf '%s\n' '{"name": "npmprobe", "version": "1.0.0", "files": ["*.{js,md}"]}' > package.json
touch index.js notes.md left-out.txt
packed="$(npm pack --dry-run --json)"
case "$packed" in *'"index.js"'*'"notes.md"'*) ;; *) echo "npm pack did not expand *.{js,md}: $packed"; exit 1;; esac
case "$packed" in *left-out.txt*) echo "npm pack packed a file *.{js,md} excludes: $packed"; exit 1;; esac`

// installProofPackage is the package fullControlProbe installs to prove a
// worker can install one. It must be a package the image does not carry, or
// apt installs nothing and the proof proves nothing (go-quicbaas#1431); the
// Dockerfile's sudo proof uses the same package for the same reason. Debian's
// hello is a leaf nothing depends on, and it has a version command.
const installProofPackage = "hello"

// fullControlProbe: the container is the boundary, so the worker has full
// control inside it (go-quicbaas#927). A worker that cannot install a package
// cannot finish a task whose toolchain the image does not carry. Proved as the
// spawn's uid, because sudo resolves the invoking user through getpwuid and
// refuses outright for a uid with no /etc/passwd entry.
//
// AND PROVED UNDER THE BOUNDARY PRODUCTION DRAWS AROUND IT (go-quicbaas#1040):
// the probe runs Sandboxed, so a missing add-back fails here as the command it
// breaks — sudo's setresuid, apt's drop to its _apt sandbox user, apt's chown
// — rather than as a stalled delivery.
// Root must hold exactly CHOWN, DAC_OVERRIDE, FOWNER, SETGID and SETUID (0xcb):
// no NET_RAW, no MKNOD, and no device node can be created.
const fullControlProbe = `sudo -n true
test "$(sudo -n id -u)" = "0" || { echo "sudo did not reach root"; exit 1; }
sudo -n apt-get update -q; sudo -n apt-get install -y -q --no-install-recommends ` + installProofPackage + `; ` + installProofPackage + ` --version
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

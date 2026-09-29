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

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The packaged deployment artifacts under deployments/fleet-node are
// hand-maintained: nothing generates them from the daemon's flag set, so a
// flag removed from `quicbaas fleet serve` leaves them naming an argument
// the binary rejects at exec ("unknown flag"), which the unit's restart
// policy then retries until the start limit trips. That is how --central
// and --enrollment-token survived their own removal (go-quicbaas#661).
//
// These tests close the loop from the other side: they parse the daemon
// invocation out of each checked-in artifact and hold it against the live
// cobra command, so a flag rename breaks the build instead of the fleet.

// deploymentDir is the packaged agent-daemon artifact directory, relative
// to this package's directory (go test runs with the package as cwd).
const deploymentDir = "../../deployments/fleet-node"

// fleetNodeUnitFile is the packaged systemd unit operators install.
const fleetNodeUnitFile = "quicbaas-agent.service"

// fleetNodeDockerfile is the packaged container image definition.
const fleetNodeDockerfile = "Dockerfile"

// fleetNodeEnvSample is the sample EnvironmentFile the packaged unit
// reads and the container image consumes as `docker run -e` variables.
const fleetNodeEnvSample = "agent.env.sample"

// invocationParseError reports a deployment artifact whose daemon
// invocation could not be located.
type invocationParseError struct {
	// File is the artifact that was parsed.
	File string
	// Directive is the instruction the parser looked for.
	Directive string
}

// Error implements error.
func (e *invocationParseError) Error() string {
	return "deployment artifact " + e.File + ": no " + e.Directive + " directive found"
}

// joinContinuations collapses backslash-continued lines into single
// logical lines. Both systemd unit files and Dockerfiles use the same
// trailing-backslash continuation, so one helper serves both parsers.
func joinContinuations(content string) []string {
	normalized := strings.ReplaceAll(content, "\\\n", " ")
	return strings.Split(normalized, "\n")
}

// systemdExecStart returns the ExecStart command line of a systemd unit,
// with continuations already joined and the directive name stripped.
func systemdExecStart(unit string) (string, error) {
	const directive = "ExecStart="
	for _, line := range joinContinuations(unit) {
		if strings.HasPrefix(strings.TrimSpace(line), directive) {
			return strings.TrimPrefix(strings.TrimSpace(line), directive), nil
		}
	}
	return "", &invocationParseError{File: fleetNodeUnitFile, Directive: "ExecStart="}
}

// dockerfileCMD returns the Dockerfile's CMD instruction with
// continuations joined and the instruction name stripped.
//
// A HEALTHCHECK also carries a CMD, but only as a continuation of the
// HEALTHCHECK line; joining continuations first is what keeps the two
// apart, because the healthcheck's logical line starts with HEALTHCHECK.
func dockerfileCMD(dockerfile string) (string, error) {
	for _, line := range joinContinuations(dockerfile) {
		if strings.HasPrefix(line, "CMD ") {
			return strings.TrimPrefix(line, "CMD "), nil
		}
	}
	return "", &invocationParseError{File: fleetNodeDockerfile, Directive: "CMD"}
}

// invocationFlagPattern matches a long flag token anywhere in a command
// line. Scanning rather than tokenizing is deliberate: both artifacts wrap
// their optional flags in shell expansions such as
// ${QUICBAAS_AUTO_PULL:+--auto-pull}, which no argv splitter would surface.
var invocationFlagPattern = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// invocationFlags returns the sorted, de-duplicated set of long flags
// named anywhere in a daemon command line.
func invocationFlags(commandLine string) []string {
	seen := map[string]struct{}{}
	for _, match := range invocationFlagPattern.FindAllString(commandLine, -1) {
		seen[strings.TrimPrefix(match, "--")] = struct{}{}
	}
	flags := make([]string, 0, len(seen))
	for name := range seen {
		flags = append(flags, name)
	}
	slices.Sort(flags)
	return flags
}

// envAssignmentPattern matches a KEY=value assignment at the start of a
// line, commented out or not. The sample file ships most of its entries
// commented so an operator uncomments what they need; a commented entry
// is still documentation of a variable the artifacts read.
var envAssignmentPattern = regexp.MustCompile(`(?m)^#?\s*([A-Z][A-Z0-9_]*)=`)

// envReferencePattern matches a ${NAME} reference, tolerating the shell's
// ${NAME:-default} and ${NAME:+alternate} forms and systemd's doubled $$.
var envReferencePattern = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)`)

// quicbaasEnvNames filters a name set down to the daemon's own QUICBAAS_
// variables and returns them sorted and de-duplicated.
func quicbaasEnvNames(matches [][]string) []string {
	seen := map[string]struct{}{}
	for _, m := range matches {
		if strings.HasPrefix(m[1], "QUICBAAS_") {
			seen[m[1]] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// envFileVariables returns the QUICBAAS_ variables an environment file
// documents, whether the entry is active or commented out.
func envFileVariables(content string) []string {
	return quicbaasEnvNames(envAssignmentPattern.FindAllStringSubmatch(content, -1))
}

// envFileReferences returns the QUICBAAS_ variables a command line reads.
func envFileReferences(commandLine string) []string {
	return quicbaasEnvNames(envReferencePattern.FindAllStringSubmatch(commandLine, -1))
}

// requiredFleetServeFlags returns the flags cobra will refuse to run
// `fleet serve` without. A packaged artifact that omits one of these
// fails at startup just as loudly as one that names a removed flag.
func requiredFleetServeFlags(cmd *cobra.Command) []string {
	var required []string
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if values, ok := flag.Annotations[cobra.BashCompOneRequiredFlag]; ok {
			for _, v := range values {
				if v == "true" {
					required = append(required, flag.Name)
				}
			}
		}
	})
	slices.Sort(required)
	return required
}

// readDeploymentArtifact loads a packaged artifact, failing the test when
// it is missing — a deleted artifact must not silently pass the gate.
func readDeploymentArtifact(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(deploymentDir, name))
	if err != nil {
		t.Fatalf("read deployment artifact %s: %v", name, err)
	}
	return string(content)
}

// assertFlagsAccepted checks every flag named in a packaged artifact --
// an invocation or the sample environment file's documentation of one --
// against the live `fleet serve` command.
func assertFlagsAccepted(t *testing.T, artifact string, commandLine string) {
	t.Helper()
	cmd := newFleetServeCommand()
	for _, name := range invocationFlags(commandLine) {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("%s names `fleet serve --%s`, which the command does not accept; "+
				"update the artifact to the current flag set", artifact, name)
		}
	}
}

// assertRequiredFlagsPresent checks that a packaged invocation supplies
// every flag cobra marks required.
func assertRequiredFlagsPresent(t *testing.T, artifact string, commandLine string) {
	t.Helper()
	cmd := newFleetServeCommand()
	present := map[string]struct{}{}
	for _, name := range invocationFlags(commandLine) {
		present[name] = struct{}{}
	}
	for _, name := range requiredFleetServeFlags(cmd) {
		if _, ok := present[name]; !ok {
			t.Errorf("%s omits required flag --%s; the daemon exits at startup without it", artifact, name)
		}
	}
}

// TestPackagedSystemdUnitInvocationMatchesCLI is the drift gate for the
// unit operators install on a bare host.
//
// Not parallel, and holds withServeGlobals: newFleetServeCommand binds the
// package-level serve* variables, so two goroutines building it concurrently
// is a data race the detector reports on every test in the package.
func TestPackagedSystemdUnitInvocationMatchesCLI(t *testing.T) {
	withServeGlobals(t)

	unit := readDeploymentArtifact(t, fleetNodeUnitFile)
	execStart, err := systemdExecStart(unit)
	if err != nil {
		t.Fatalf("parse ExecStart: %v", err)
	}
	if !strings.Contains(execStart, "quicbaas fleet serve") {
		t.Fatalf("ExecStart does not invoke `quicbaas fleet serve`: %s", execStart)
	}

	assertFlagsAccepted(t, fleetNodeUnitFile, execStart)
	assertRequiredFlagsPresent(t, fleetNodeUnitFile, execStart)
}

// TestPackagedDockerfileInvocationMatchesCLI is the same gate for the
// container image's default command. Sequential for the same reason as the
// unit gate above.
func TestPackagedDockerfileInvocationMatchesCLI(t *testing.T) {
	withServeGlobals(t)

	dockerfile := readDeploymentArtifact(t, fleetNodeDockerfile)
	cmdLine, err := dockerfileCMD(dockerfile)
	if err != nil {
		t.Fatalf("parse CMD: %v", err)
	}
	if !strings.Contains(cmdLine, "quicbaas fleet serve") {
		t.Fatalf("CMD does not invoke `quicbaas fleet serve`: %s", cmdLine)
	}

	assertFlagsAccepted(t, fleetNodeDockerfile, cmdLine)
	assertRequiredFlagsPresent(t, fleetNodeDockerfile, cmdLine)
}

// TestPackagedEnvSampleFlagsMatchCLI is the drift gate for the sample
// environment file. It documents each variable by the flag that variable
// feeds, so a renamed flag leaves it telling operators to configure
// something the daemon does not have — the shape of go-quicbaas#691,
// where the file still described a `--config` flag `fleet serve` never
// grew and a YAML loader that does not exist.
func TestPackagedEnvSampleFlagsMatchCLI(t *testing.T) {
	withServeGlobals(t)

	sample := readDeploymentArtifact(t, fleetNodeEnvSample)
	named := invocationFlags(sample)
	if len(named) == 0 {
		t.Fatal("sample environment file names no flags; it no longer documents what each variable configures")
	}
	assertFlagsAccepted(t, fleetNodeEnvSample, sample)
}

// TestPackagedEnvSampleCoversEveryVariableTheArtifactsRead pins the
// sample and the two invocations to each other in both directions: a
// variable the unit or image expands must be documented, and a variable
// the sample documents must actually be read. Either gap strands an
// operator — the first with a setting they cannot discover, the second
// with one they set and the daemon ignores.
func TestPackagedEnvSampleCoversEveryVariableTheArtifactsRead(t *testing.T) {
	t.Parallel()

	sample := readDeploymentArtifact(t, fleetNodeEnvSample)
	unit := readDeploymentArtifact(t, fleetNodeUnitFile)
	dockerfile := readDeploymentArtifact(t, fleetNodeDockerfile)

	execStart, err := systemdExecStart(unit)
	if err != nil {
		t.Fatalf("parse ExecStart: %v", err)
	}
	cmdLine, err := dockerfileCMD(dockerfile)
	if err != nil {
		t.Fatalf("parse CMD: %v", err)
	}

	documented := envFileVariables(sample)
	if len(documented) == 0 {
		t.Fatal("sample environment file documents no QUICBAAS_ variables")
	}

	read := map[string]struct{}{}
	for _, name := range append(envFileReferences(execStart), envFileReferences(cmdLine)...) {
		read[name] = struct{}{}
	}

	for name := range read {
		if !slices.Contains(documented, name) {
			t.Errorf("%s and %s expand %s, but %s does not document it",
				fleetNodeUnitFile, fleetNodeDockerfile, name, fleetNodeEnvSample)
		}
	}
	for _, name := range documented {
		if _, ok := read[name]; !ok {
			t.Errorf("%s documents %s, but neither %s nor %s reads it",
				fleetNodeEnvSample, name, fleetNodeUnitFile, fleetNodeDockerfile)
		}
	}
}

// TestEnvFileParsersRejectUndocumentedInput covers the two environment
// parsers' bad-input paths: content with no assignments and a command
// line with no references must both come back empty rather than
// reporting names scraped out of prose.
func TestEnvFileParsersRejectUndocumentedInput(t *testing.T) {
	t.Parallel()

	const prose = "# QuicBaaS Agent Daemon\n# Set KEY=value, one per line.\n# See QUICBAAS_SERVER in the unit.\n"
	if got := envFileVariables(prose); len(got) != 0 {
		t.Errorf("envFileVariables(prose) = %v, want none", got)
	}
	const noRefs = "quicbaas fleet serve --server https://host:8443 --tenant acme"
	if got := envFileReferences(noRefs); len(got) != 0 {
		t.Errorf("envFileReferences(%q) = %v, want none", noRefs, got)
	}
}

// TestEnvFileParsersReadBothEntryForms checks the parsers see a commented
// entry as documentation and read a reference through both the shell
// default form and systemd's doubled dollar.
func TestEnvFileParsersReadBothEntryForms(t *testing.T) {
	t.Parallel()

	const sample = "QUICBAAS_SERVER=https://host:8443\n#QUICBAAS_BIND_ADDR=0.0.0.0:9091\nOTHER_VAR=ignored\n"
	got := envFileVariables(sample)
	want := []string{"QUICBAAS_BIND_ADDR", "QUICBAAS_SERVER"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("envFileVariables = %v, want %v", got, want)
	}

	const refs = `--server "$${QUICBAAS_SERVER}" --bind "${QUICBAAS_BIND_ADDR:-0.0.0.0:9091}" ${QUICBAAS_AUTO_PULL:+--auto-pull}`
	gotRefs := envFileReferences(refs)
	wantRefs := []string{"QUICBAAS_AUTO_PULL", "QUICBAAS_BIND_ADDR", "QUICBAAS_SERVER"}
	if strings.Join(gotRefs, ",") != strings.Join(wantRefs, ",") {
		t.Errorf("envFileReferences = %v, want %v", gotRefs, wantRefs)
	}
}

// TestInvocationFlagsRejectsRemovedFlags proves the gate has teeth by
// replaying the exact ExecStart that shipped with go-quicbaas#661: the
// removed flags must be reported as unknown, and the required flags the
// old invocation never supplied must be reported as missing. Sequential
// because it too builds the command.
func TestInvocationFlagsRejectsRemovedFlags(t *testing.T) {
	withServeGlobals(t)

	const brokenExecStart = `/bin/sh -c '/usr/local/bin/quicbaas fleet serve ` +
		`--central "${QUICBAAS_CENTRAL}" ` +
		`--tenant "${QUICBAAS_TENANT}" ` +
		`--bind "${QUICBAAS_BIND_ADDR:-0.0.0.0:9091}" ` +
		`--work-dir /var/lib/quicbaas-agent ` +
		`${QUICBAAS_ENROLLMENT_TOKEN:+--enrollment-token "${QUICBAAS_ENROLLMENT_TOKEN}"}'`

	cmd := newFleetServeCommand()
	named := invocationFlags(brokenExecStart)

	for _, removed := range []string{"central", "enrollment-token"} {
		if !slices.Contains(named, removed) {
			t.Fatalf("flag scanner missed --%s in %q", removed, brokenExecStart)
		}
		if cmd.Flags().Lookup(removed) != nil {
			t.Fatalf("--%s is back on `fleet serve`; the packaged artifacts and this test both assume it is gone", removed)
		}
	}

	present := map[string]struct{}{}
	for _, name := range named {
		present[name] = struct{}{}
	}
	var missing []string
	for _, name := range requiredFleetServeFlags(cmd) {
		if _, ok := present[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		t.Fatal("required-flag check found nothing missing in the pre-fix invocation, which supplied no mTLS material")
	}
}

// TestSystemdExecStartRejectsUnitWithoutDirective covers the parser's
// error path: a unit with no ExecStart must be reported, not silently
// treated as an invocation with zero flags.
func TestSystemdExecStartRejectsUnitWithoutDirective(t *testing.T) {
	t.Parallel()

	const unit = "[Unit]\nDescription=no service section\n\n[Install]\nWantedBy=multi-user.target\n"
	if _, err := systemdExecStart(unit); err == nil {
		t.Fatal("expected an error for a unit with no ExecStart")
	} else if !strings.Contains(err.Error(), "ExecStart=") {
		t.Fatalf("error does not name the missing directive: %v", err)
	}
}

// TestDockerfileCMDRejectsFileWithoutDirective covers the same error path
// for Dockerfiles, including the HEALTHCHECK trap: a healthcheck's own CMD
// continuation must not be mistaken for the image's default command.
func TestDockerfileCMDRejectsFileWithoutDirective(t *testing.T) {
	t.Parallel()

	const dockerfile = "FROM alpine:3.21\n" +
		"HEALTHCHECK --interval=30s \\\n    CMD quicbaas --help > /dev/null 2>&1 || exit 1\n" +
		"ENTRYPOINT [\"/usr/local/bin/quicbaas\"]\n"
	if _, err := dockerfileCMD(dockerfile); err == nil {
		t.Fatal("expected an error for a Dockerfile whose only CMD belongs to HEALTHCHECK")
	} else if !strings.Contains(err.Error(), "CMD") {
		t.Fatalf("error does not name the missing directive: %v", err)
	}
}

// TestInvocationFlagsIgnoresValuesAndComments checks the scanner reports
// flag names only — not the values, paths, or image references beside
// them — so a value containing a dash never masquerades as a flag.
func TestInvocationFlagsIgnoresValuesAndComments(t *testing.T) {
	t.Parallel()

	const line = `quicbaas fleet serve --server "https://host:8443" ` +
		`--image-map "claude-code=ghcr.io/x/claude-code:v1" --work-dir /var/lib/quicbaas-agent/work`
	got := invocationFlags(line)
	want := []string{"image-map", "server", "work-dir"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("invocationFlags(%q) = %v, want %v", line, got, want)
	}
}

// TestPackagedSystemdUnitSignalsTheDaemonAlone pins go-quicbaas#1064 on the
// packaged unit: SIGTERM must reach the daemon alone so it drains its
// in-flight turns before the `docker run` clients behind them are signalled.
func TestPackagedSystemdUnitSignalsTheDaemonAlone(t *testing.T) {
	unit := readDeploymentArtifact(t, fleetNodeUnitFile)
	if !strings.Contains(unit, "\nKillMode=mixed\n") {
		t.Errorf("%s must set KillMode=mixed so a stop drains before it closes a worker's stream", fleetNodeUnitFile)
	}
}

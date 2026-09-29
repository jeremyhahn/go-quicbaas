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

package orchestration

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// =============================================================================
// The startup sweep (go-quicbaas#750)
// =============================================================================

// fakeRuntimeCLI is a docker/podman stand-in whose whole world is one
// directory: a file per container, named for its id and holding the value of
// its ContainerOwnerLabel (empty for a container carrying no such label).
//
// It is a real executable rather than an interface double because the sweep's
// contract is the ARGUMENT VECTOR it hands a container runtime — a filter that
// named the wrong label, or an `rm` that named the wrong container, would go
// unnoticed against a double that answered a Go method call.
type fakeRuntimeCLI struct {
	t    *testing.T
	root string
	path string

	// psExit and rmExit make the two halves of the sweep fail on demand;
	// inspectExit makes the reaper's describe step fail.
	psExit      int
	rmExit      int
	inspectExit int
}

func newFakeRuntimeCLI(t *testing.T) *fakeRuntimeCLI {
	t.Helper()
	dir := t.TempDir()
	f := &fakeRuntimeCLI{
		t:    t,
		root: filepath.Join(dir, "containers"),
		path: filepath.Join(dir, "fake-runtime"),
	}
	if err := os.MkdirAll(f.root, 0o700); err != nil {
		t.Fatalf("container root: %v", err)
	}
	f.write()
	return f
}

// write regenerates the script, so a test that arms a failure after
// construction gets it.
func (f *fakeRuntimeCLI) write() {
	f.t.Helper()
	script := "#!/bin/sh\n" +
		"ROOT='" + f.root + "'\n" +
		"cmd=$1; shift\n" +
		"case \"$cmd\" in\n" +
		"ps)\n" +
		"  [ " + strconv.Itoa(f.psExit) + " -ne 0 ] && { echo 'cannot connect to the daemon' >&2; exit " + strconv.Itoa(f.psExit) + "; }\n" +
		"  filter=''\n" +
		"  while [ $# -gt 0 ]; do\n" +
		"    if [ \"$1\" = '--filter' ]; then shift; filter=\"$1\"; fi\n" +
		"    shift\n" +
		"  done\n" +
		"  want=${filter#label=" + ContainerOwnerLabel + "=}\n" +
		"  [ \"$want\" = \"$filter\" ] && { echo 'unexpected filter' >&2; exit 2; }\n" +
		"  for entry in \"$ROOT\"/*; do\n" +
		"    [ -e \"$entry\" ] || continue\n" +
		"    [ \"$(cat \"$entry\")\" = \"$want\" ] && basename \"$entry\"\n" +
		"  done\n" +
		"  exit 0\n" +
		"  ;;\n" +
		"inspect)\n" +
		"  [ " + strconv.Itoa(f.inspectExit) + " -ne 0 ] && { echo 'cannot connect to the daemon' >&2; exit " + strconv.Itoa(f.inspectExit) + "; }\n" +
		"  [ \"$1\" = '--format' ] || { echo 'inspect without --format' >&2; exit 2; }\n" +
		"  shift 2\n" +
		"  for id in \"$@\"; do cat \"$ROOT.meta/$id\" 2>/dev/null; done\n" +
		"  exit 0\n" +
		"  ;;\n" +
		"rm)\n" +
		"  [ " + strconv.Itoa(f.rmExit) + " -ne 0 ] && { echo 'device or resource busy' >&2; exit " + strconv.Itoa(f.rmExit) + "; }\n" +
		"  shift\n" +
		"  rm -f \"$ROOT/$1\"\n" +
		"  exit 0\n" +
		"  ;;\n" +
		"esac\n" +
		"echo \"unexpected subcommand $cmd\" >&2\n" +
		"exit 3\n"
	if err := os.WriteFile(f.path, []byte(script), 0o700); err != nil {
		f.t.Fatalf("write fake runtime: %v", err)
	}
}

// add plants a container with the given owner label; an empty owner is a
// container carrying no owner label at all.
func (f *fakeRuntimeCLI) add(id, owner string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, id), []byte(owner), 0o600); err != nil {
		f.t.Fatalf("plant container %s: %v", id, err)
	}
}

// describe plants what inspect reports for a container: its creation time and
// identity labels, in ListOwned's tab-separated format.
func (f *fakeRuntimeCLI) describe(id, created, session, job, tenant string) {
	f.t.Helper()
	dir := f.root + ".meta"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatalf("meta root: %v", err)
	}
	line := id + "\t/" + id + "\t" + created + "\t" + session + "\t" + job + "\t" + tenant + "\n"
	if err := os.WriteFile(filepath.Join(dir, id), []byte(line), 0o600); err != nil {
		f.t.Fatalf("describe container %s: %v", id, err)
	}
}

// remaining is every container the fake runtime still holds, sorted.
func (f *fakeRuntimeCLI) remaining() []string {
	f.t.Helper()
	entries, err := os.ReadDir(f.root)
	if err != nil {
		f.t.Fatalf("read containers: %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func (f *fakeRuntimeCLI) runtime() *ExecContainerRuntime {
	return &ExecContainerRuntime{command: f.path, logger: newContainerTestLogger(f.t)}
}

// TestSweepOwned_RemovesThisDaemonsOrphansAndNothingElse is the sweep's whole
// contract: a container labelled for this daemon is an orphan at startup, and
// everything else on the host is somebody else's.
func TestSweepOwned_RemovesThisDaemonsOrphansAndNothingElse(t *testing.T) {
	fake := newFakeRuntimeCLI(t)
	const owner = "/var/lib/quicbaas-agent"
	fake.add("quicbaas-acp-aaaa", owner)
	fake.add("quicbaas-acp-bbbb", owner)
	// A worker belonging to a DIFFERENT daemon on the same host.
	fake.add("quicbaas-acp-cccc", "/var/lib/quicbaas-agent-two")
	// A container carrying no owner label at all — an operator's own.
	fake.add("postgres-dev", "")

	sweep, err := fake.runtime().SweepOwned(t.Context(), owner)
	if err != nil {
		t.Fatalf("SweepOwned: %v", err)
	}
	if sweep.Owner != owner {
		t.Errorf("sweep.Owner = %q, want %q", sweep.Owner, owner)
	}
	sort.Strings(sweep.Removed)
	if strings.Join(sweep.Removed, ",") != "quicbaas-acp-aaaa,quicbaas-acp-bbbb" {
		t.Errorf("removed = %v, want this daemon's two orphans", sweep.Removed)
	}
	if got := strings.Join(fake.remaining(), ","); got != "postgres-dev,quicbaas-acp-cccc" {
		t.Fatalf("the sweep removed what was not its own; left = %s", got)
	}
}

// TestSweepOwned_AHostWithNothingOfItsOwnIsANoOp: the ordinary startup. It
// must report an empty sweep rather than an error, or every clean boot would
// be a failure.
func TestSweepOwned_AHostWithNothingOfItsOwnIsANoOp(t *testing.T) {
	fake := newFakeRuntimeCLI(t)
	fake.add("postgres-dev", "")

	sweep, err := fake.runtime().SweepOwned(t.Context(), "/var/lib/quicbaas-agent")
	if err != nil {
		t.Fatalf("SweepOwned: %v", err)
	}
	if len(sweep.Removed) != 0 {
		t.Fatalf("removed = %v, want nothing", sweep.Removed)
	}
	if got := strings.Join(fake.remaining(), ","); got != "postgres-dev" {
		t.Fatalf("a no-op sweep touched the host; left = %s", got)
	}
}

// TestSweepOwned_RefusesAnEmptyOwner is the bad-input case, and the one that
// matters most: a sweep with no owner could only be a sweep of every container
// on the host.
func TestSweepOwned_RefusesAnEmptyOwner(t *testing.T) {
	fake := newFakeRuntimeCLI(t)
	fake.add("quicbaas-acp-aaaa", "")

	for _, owner := range []string{"", "   ", "\t\n"} {
		sweep, err := fake.runtime().SweepOwned(t.Context(), owner)
		var refused *ContainerSweepRefusedError
		if !errors.As(err, &refused) {
			t.Fatalf("SweepOwned(%q) err = %v, want ContainerSweepRefusedError", owner, err)
		}
		if !errors.Is(err, ErrContainerSweep) {
			t.Fatalf("SweepOwned(%q) does not unwrap to ErrContainerSweep", owner)
		}
		if len(sweep.Removed) != 0 {
			t.Fatalf("a refused sweep removed %v", sweep.Removed)
		}
	}
	if got := strings.Join(fake.remaining(), ","); got != "quicbaas-acp-aaaa" {
		t.Fatalf("a refused sweep touched the host; left = %s", got)
	}
}

// TestSweepOwned_ReportsAnEnumerationItCouldNotMake: a runtime that will not
// say what it is holding leaves the daemon unable to claim anything, and a
// sweep that could not run is not a sweep that found nothing.
func TestSweepOwned_ReportsAnEnumerationItCouldNotMake(t *testing.T) {
	fake := newFakeRuntimeCLI(t)
	fake.psExit = 1
	fake.write()

	_, err := fake.runtime().SweepOwned(t.Context(), "/var/lib/quicbaas-agent")
	var swept *ContainerSweepError
	if !errors.As(err, &swept) {
		t.Fatalf("err = %v, want ContainerSweepError", err)
	}
	if swept.ContainerID != "" {
		t.Errorf("an enumeration failure named a container: %q", swept.ContainerID)
	}
}

// TestSweepOwned_ReportsAnOrphanItCouldNotRemove is the second half: an
// identified orphan that refuses to go is the accumulation the sweep exists to
// stop, and reporting success over it would hide exactly that.
func TestSweepOwned_ReportsAnOrphanItCouldNotRemove(t *testing.T) {
	fake := newFakeRuntimeCLI(t)
	const owner = "/var/lib/quicbaas-agent"
	fake.add("quicbaas-acp-aaaa", owner)
	fake.rmExit = 1
	fake.write()

	_, err := fake.runtime().SweepOwned(t.Context(), owner)
	var swept *ContainerSweepError
	if !errors.As(err, &swept) {
		t.Fatalf("err = %v, want ContainerSweepError", err)
	}
	if swept.ContainerID != "quicbaas-acp-aaaa" {
		t.Errorf("the failure did not name the orphan: %q", swept.ContainerID)
	}
	if got := strings.Join(fake.remaining(), ","); got != "quicbaas-acp-aaaa" {
		t.Fatalf("the orphan is gone after a removal that failed; left = %s", got)
	}
}

// TestSandboxSpec_StampsTheOwnerLabelOnEverySandboxContainer: the sweep can
// only be exact if the launch is labelled, and the label has to be on the SPEC
// rather than added at launch — ContainerConfigDigest measures spec.Labels, so
// a label the runtime injected below the measurement would make an attested
// container disagree with the configuration that was attested.
func TestSandboxSpec_StampsTheOwnerLabelOnEverySandboxContainer(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "claude", CLIMode: true})
	backend.SetSandboxOwner("  /var/lib/quicbaas-agent  ")

	spec, err := backend.sandboxSpec(&SpawnRequest{WorkDir: WorkDirCurrent}, testContainerIdentity, "worker:1", []string{"claude"})
	if err != nil {
		t.Fatalf("sandboxSpec: %v", err)
	}
	if got := spec.Labels[ContainerOwnerLabel]; got != "/var/lib/quicbaas-agent" {
		t.Fatalf("owner label = %q, want the trimmed work directory", got)
	}
	if spec.Labels["quicbaas.agent"] == "" {
		t.Error("the owner label displaced the agent label")
	}

	digest, digestErr := ContainerConfigDigest(spec)
	if digestErr != nil {
		t.Fatalf("ContainerConfigDigest: %v", digestErr)
	}
	if digest == "" {
		t.Fatal("the labelled spec does not measure")
	}
}

// TestSandboxSpec_AnUnownedBackendStampsNoOwnerLabel is the bad-input twin: a
// backend nothing told which daemon it serves must leave the label off, so no
// sweep can ever claim its containers. An unswept container is an operator's
// problem; a container the wrong process sweeps is a running worker killed
// under someone else.
func TestSandboxSpec_AnUnownedBackendStampsNoOwnerLabel(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "claude", CLIMode: true})

	spec, err := backend.sandboxSpec(&SpawnRequest{WorkDir: WorkDirCurrent}, testContainerIdentity, "worker:1", []string{"claude"})
	if err != nil {
		t.Fatalf("sandboxSpec: %v", err)
	}
	if _, ok := spec.Labels[ContainerOwnerLabel]; ok {
		t.Fatalf("an unowned backend stamped %q", spec.Labels[ContainerOwnerLabel])
	}
}

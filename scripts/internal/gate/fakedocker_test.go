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

package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFakeInvocationRecognisesOnlyTheFakedNames(t *testing.T) {
	cases := []struct {
		argv0    string
		wantName string
		wantFake bool
	}{
		{argv0: "/tmp/w/bin/docker", wantName: "docker", wantFake: true},
		{argv0: "docker", wantName: "docker", wantFake: true},
		{argv0: "/tmp/w/bin/nvidia-smi", wantName: "nvidia-smi", wantFake: true},
		// The gate's own name must never be read as a fake, or `go run` would
		// answer as a daemon instead of running the gate.
		{argv0: "/tmp/go-build123/b001/exe/verifycleandocker", wantName: "verifycleandocker", wantFake: false},
		{argv0: "/usr/bin/docker-compose", wantName: "docker-compose", wantFake: false},
	}
	for _, tc := range cases {
		t.Run(tc.argv0, func(t *testing.T) {
			name, isFake := FakeInvocation(tc.argv0)
			if name != tc.wantName || isFake != tc.wantFake {
				t.Fatalf("FakeInvocation(%q) = (%q, %v), want (%q, %v)",
					tc.argv0, name, isFake, tc.wantName, tc.wantFake)
			}
		})
	}
}

func TestRunFakeServesTheDaemonTheCaseDescribed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(FakeDirEnv, dir)
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	write("images.out", "quicbaas:latest\nsomeone-elses/image:latest\n")
	write("images.rc", "0\n")
	write("rmi.fail", "quicbaas:stuck\n")

	t.Run("images prints the fixture and exits its status", func(t *testing.T) {
		var stdout, stderr strings.Builder
		if status := RunFake("docker", []string{"images", "--format", "{{.Repository}}"}, &stdout, &stderr); status != 0 {
			t.Fatalf("docker images exited %d, want 0", status)
		}
		if !strings.Contains(stdout.String(), "quicbaas:latest") {
			t.Fatalf("docker images printed %q", stdout.String())
		}
	})

	t.Run("an unreachable daemon is reported as a failure", func(t *testing.T) {
		write("images.rc", "1\n")
		var stdout, stderr strings.Builder
		if status := RunFake("docker", []string{"images"}, &stdout, &stderr); status != 1 {
			t.Fatalf("docker images exited %d, want 1", status)
		}
		write("images.rc", "0\n")
	})

	t.Run("a removal the case marked failing fails", func(t *testing.T) {
		var stdout, stderr strings.Builder
		if status := RunFake("docker", []string{"rmi", "quicbaas:stuck"}, &stdout, &stderr); status != 1 {
			t.Fatalf("docker rmi of a stuck tag exited %d, want 1", status)
		}
		if !strings.Contains(stderr.String(), "unable to remove quicbaas:stuck") {
			t.Fatalf("the refusal does not name the tag: %q", stderr.String())
		}
	})

	t.Run("a removal the case did not mark succeeds", func(t *testing.T) {
		var stdout, stderr strings.Builder
		if status := RunFake("docker", []string{"rmi", "quicbaas:latest"}, &stdout, &stderr); status != 0 {
			t.Fatalf("docker rmi of a removable tag exited %d, want 0", status)
		}
	})

	t.Run("fail.match refuses the step the case chose", func(t *testing.T) {
		write("fail.match", "build --no-cache\n")
		var stdout, stderr strings.Builder
		status := RunFake("docker", []string{"compose", "-f", "x.yml", "build", "--no-cache"}, &stdout, &stderr)
		if status != 1 {
			t.Fatalf("the refused step exited %d, want 1", status)
		}
		if !strings.Contains(stderr.String(), "refusing") {
			t.Fatalf("the refusal was not reported: %q", stderr.String())
		}
		// Another step is unaffected.
		if status := RunFake("docker", []string{"compose", "-f", "x.yml", "up"}, &stdout, &stderr); status != 0 {
			t.Fatalf("an unrefused step exited %d, want 0", status)
		}
		write("fail.match", "")
	})

	t.Run("every invocation is logged", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(dir, "calls.log"))
		if err != nil {
			t.Fatalf("reading the call log: %v", err)
		}
		log := string(raw)
		for _, want := range []string{
			"images --format {{.Repository}}",
			"rmi quicbaas:stuck",
			"compose -f x.yml build --no-cache",
		} {
			if !strings.Contains(log, want) {
				t.Fatalf("the call log is missing %q:\n%s", want, log)
			}
		}
	})
}

func TestRunFakeRefusesToActAsADaemonWithoutItsFixture(t *testing.T) {
	// Something other than a gate resolved this binary as docker. Answering 0
	// would make a real recipe believe a daemon agreed with it.
	t.Setenv(FakeDirEnv, "")
	var stdout, stderr strings.Builder
	if status := RunFake("docker", []string{"images"}, &stdout, &stderr); status != ExitToolError {
		t.Fatalf("the unconfigured fake exited %d, want %d", status, ExitToolError)
	}
	if !strings.Contains(stderr.String(), FakeDirEnv) {
		t.Fatalf("the error does not name the missing input: %q", stderr.String())
	}
}

func TestRunFakeNvidiaSmiIsPresentButNonFunctional(t *testing.T) {
	// A recipe probing for a GPU must get the same answer on every runner, or
	// the gate's verdict depends on the hardware it happened to land on.
	var stdout, stderr strings.Builder
	if status := RunFake("nvidia-smi", []string{"--query-gpu=name"}, &stdout, &stderr); status != 1 {
		t.Fatalf("the nvidia-smi fake exited %d, want 1", status)
	}
}

func TestRunFakeRefusesAnUnreadableStatusFixture(t *testing.T) {
	// Defaulting to success here would silently retire the "unreachable daemon"
	// case, which is the one that proves cleanup fails closed.
	dir := t.TempDir()
	t.Setenv(FakeDirEnv, dir)
	if err := os.WriteFile(filepath.Join(dir, "images.rc"), []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatalf("writing images.rc: %v", err)
	}
	var stdout, stderr strings.Builder
	if status := RunFake("docker", []string{"images"}, &stdout, &stderr); status != ExitToolError {
		t.Fatalf("a malformed images.rc exited %d, want %d", status, ExitToolError)
	}
}

func TestInstallFakesPutsEveryFakedNameOnDisk(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := InstallFakes(binDir); err != nil {
		t.Fatalf("InstallFakes: %v", err)
	}
	for name := range fakeNames {
		path := filepath.Join(binDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s was not installed: %v", name, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("%s is not executable", name)
		}
	}
	// Installing twice must not fail: a gate that re-arms its workspace is
	// ordinary, and a leftover link is not an error.
	if err := InstallFakes(binDir); err != nil {
		t.Fatalf("re-running InstallFakes: %v", err)
	}
}

func TestDispatchFakeReturnsWhenThisIsNotAFake(t *testing.T) {
	// The gate's own invocation must fall through to the gate. The exiting
	// branch cannot be exercised in process — it is exercised for real by the
	// TestMain of every gate whose cases put this binary on PATH.
	saved := os.Args[0]
	t.Cleanup(func() { os.Args[0] = saved })
	os.Args[0] = "/tmp/go-build123/b001/exe/verifycleandocker"
	DispatchFake()
}

func TestRunFakeFailsRatherThanPretendingWhenItsFixtureIsUnusable(t *testing.T) {
	// Each of these would otherwise let the fake answer 0 and tell a real
	// recipe that a daemon agreed with it.
	t.Run("the call log cannot be written", func(t *testing.T) {
		t.Setenv(FakeDirEnv, filepath.Join(t.TempDir(), "does-not-exist"))
		var stdout, stderr strings.Builder
		if status := RunFake("docker", []string{"images"}, &stdout, &stderr); status != ExitToolError {
			t.Fatalf("an unwritable call log exited %d, want %d", status, ExitToolError)
		}
		if !strings.Contains(stderr.String(), "cannot log the call") {
			t.Fatalf("the failure was not reported: %q", stderr.String())
		}
	})

	t.Run("the image list cannot be read", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(FakeDirEnv, dir)
		// A directory where a file belongs: present, and unreadable as one.
		if err := os.Mkdir(filepath.Join(dir, "images.out"), 0o755); err != nil {
			t.Fatalf("preparing the fixture: %v", err)
		}
		var stdout, stderr strings.Builder
		if status := RunFake("docker", []string{"images"}, &stdout, &stderr); status != ExitToolError {
			t.Fatalf("an unreadable image list exited %d, want %d", status, ExitToolError)
		}
	})

	t.Run("the status file cannot be read", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(FakeDirEnv, dir)
		if err := os.Mkdir(filepath.Join(dir, "images.rc"), 0o755); err != nil {
			t.Fatalf("preparing the fixture: %v", err)
		}
		var stdout, stderr strings.Builder
		if status := RunFake("docker", []string{"images"}, &stdout, &stderr); status != ExitToolError {
			t.Fatalf("an unreadable status exited %d, want %d", status, ExitToolError)
		}
	})
}

func TestRunFakeDefaultsAreTheNeutralDaemon(t *testing.T) {
	// A gate writes only the control files its cases need, so every absent file
	// has to mean "nothing special happens".
	dir := t.TempDir()
	t.Setenv(FakeDirEnv, dir)
	var stdout, stderr strings.Builder
	if status := RunFake("docker", []string{"images"}, &stdout, &stderr); status != ExitClean {
		t.Fatalf("images with no fixture exited %d, want 0", status)
	}
	if stdout.String() != "" {
		t.Fatalf("images with no fixture printed %q", stdout.String())
	}
	if status := RunFake("docker", []string{"rmi", "anything:latest"}, &stdout, &stderr); status != ExitClean {
		t.Fatalf("rmi with no fixture exited %d, want 0", status)
	}
	if status := RunFake("docker", []string{"compose", "up"}, &stdout, &stderr); status != ExitClean {
		t.Fatalf("an unhandled subcommand exited %d, want 0", status)
	}
	if status := RunFake("docker", nil, &stdout, &stderr); status != ExitClean {
		t.Fatalf("docker with no arguments exited %d, want 0", status)
	}
}

func TestInstallFakesFailsWhenItCannotPrepareTheDirectory(t *testing.T) {
	// A gate whose fakes are not on PATH would run the recipe against the real
	// daemon, so this must fail loudly rather than carry on.
	occupied := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(occupied, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("preparing the fixture: %v", err)
	}
	if err := InstallFakes(occupied); err == nil {
		t.Fatal("InstallFakes must fail when the directory cannot be created")
	}
}

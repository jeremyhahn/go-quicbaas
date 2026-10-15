// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The package's tests never run a host binary. Where the code under test
// launches the OCI runtime, the test binary re-execs itself as a fake one
// (the Go helper-process pattern): TestMain sees fakeOCIRuntimeEnv in its
// environment and plays the runtime instead of running the tests.
const (
	fakeOCIRuntimeEnv = "CONTAINERRUNTIME_FAKE_OCI_RUNTIME"
	fakeOCIStdoutEnv  = "CONTAINERRUNTIME_FAKE_OCI_STDOUT"
	fakeOCIStderrEnv  = "CONTAINERRUNTIME_FAKE_OCI_STDERR"
	fakeOCIExitEnv    = "CONTAINERRUNTIME_FAKE_OCI_EXIT"
)

// fakeOCISubcommands are the OCI runtime subcommands the package invokes.
// Any other argv is a test bug, reported with exit status 2.
var fakeOCISubcommands = map[string]bool{
	"create": true,
	"start":  true,
	"state":  true,
	"kill":   true,
	"delete": true,
	"exec":   true,
}

// fakeOCIResult is what the fake OCI runtime prints and returns.
type fakeOCIResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func TestMain(m *testing.M) {
	if os.Getenv(fakeOCIRuntimeEnv) != "" {
		os.Exit(runFakeOCIRuntime(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// runFakeOCIRuntime plays the OCI runtime for one invocation and returns its
// exit status.
func runFakeOCIRuntime(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || !fakeOCISubcommands[args[0]] {
		fmt.Fprintf(stderr, "fake OCI runtime: unexpected arguments %q\n", args)
		return 2
	}

	exitCode, err := strconv.Atoi(os.Getenv(fakeOCIExitEnv))
	if err != nil {
		fmt.Fprintf(stderr, "fake OCI runtime: bad %s: %v\n", fakeOCIExitEnv, err)
		return 2
	}
	fmt.Fprint(stdout, os.Getenv(fakeOCIStdoutEnv))
	fmt.Fprint(stderr, os.Getenv(fakeOCIStderrEnv))
	return exitCode
}

// fakeOCIRuntime arms the fake OCI runtime with result for the rest of t and
// returns its path, for RuntimeConfig.OCIRuntime or exec.Command. The path is
// the test binary's own, which is absolute, so exec.LookPath accepts it.
func fakeOCIRuntime(t *testing.T, result fakeOCIResult) string {
	t.Helper()

	t.Setenv(fakeOCIRuntimeEnv, "1")
	t.Setenv(fakeOCIStdoutEnv, result.Stdout)
	t.Setenv(fakeOCIStderrEnv, result.Stderr)
	t.Setenv(fakeOCIExitEnv, strconv.Itoa(result.ExitCode))
	return os.Args[0]
}

func TestFakeOCIRuntime_PlaysResult(t *testing.T) {
	t.Setenv(fakeOCIStdoutEnv, `{"pid":42}`)
	t.Setenv(fakeOCIStderrEnv, "warning")
	t.Setenv(fakeOCIExitEnv, "4")

	var stdout, stderr bytes.Buffer
	code := runFakeOCIRuntime([]string{"state", "cid"}, &stdout, &stderr)
	assert.Equal(t, 4, code)
	assert.Equal(t, `{"pid":42}`, stdout.String())
	assert.Equal(t, "warning", stderr.String())
}

func TestFakeOCIRuntime_RejectsUnknownArgs(t *testing.T) {
	t.Setenv(fakeOCIExitEnv, "0")

	for _, args := range [][]string{nil, {"-c", "exit 5"}, {"run", "cid"}} {
		var stdout, stderr bytes.Buffer
		code := runFakeOCIRuntime(args, &stdout, &stderr)
		assert.Equal(t, 2, code, "args %q", args)
		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), "unexpected arguments")
	}
}

func TestFakeOCIRuntime_RejectsBadExitCode(t *testing.T) {
	t.Setenv(fakeOCIExitEnv, "not-a-number")

	var stdout, stderr bytes.Buffer
	code := runFakeOCIRuntime([]string{"state", "cid"}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), fakeOCIExitEnv)
}

func TestFakeOCIRuntime_ReexecsTestBinary(t *testing.T) {
	path := fakeOCIRuntime(t, fakeOCIResult{Stdout: `{"pid":7}`})

	out, err := exec.Command(path, "state", "cid").Output()
	require.NoError(t, err)
	assert.Equal(t, `{"pid":7}`, string(out))
}

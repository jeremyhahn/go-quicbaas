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
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// The gates that decide what a Makefile recipe DOES with Docker put a fake
// docker on PATH and read back what the recipe asked of it. Two of them need
// this — verify-clean-docker and verify-integration-teardown — so it lives here
// once.
//
// THE FAKE IS THIS BINARY. The gate symlinks its own executable into a
// directory it prepends to PATH under the name `docker`, and main() dispatches
// on argv[0]. There is no fake shell script anywhere: the whole point of
// go-quicbaas#942 is that this repository's gates are Go, and a Go gate that
// writes a bash stub to make itself work has moved the shell rather than
// removed it.
//
// The fake reads its behaviour from files the gate writes into FakeDirEnv, so a
// case chooses what the daemon looks like without the fake knowing which gate
// is running it:
//
//	calls.log   — every invocation, one space-joined argument list per line
//	fail.match  — when non-empty, any invocation containing this text fails
//	images.out  — what `docker images` prints
//	images.rc   — the status `docker images` exits with (default 0)
//	rmi.fail    — tags whose removal fails, one per line
//	watch.path  — a file whose size (-1 when absent) is appended to watch.log
//	              beside every call, as "<call>\t<bytes>"
//	hang.match  — when non-empty, a call containing this text writes itself to
//	              the FIFO hang.fifo (which the gate creates) and then waits
//	              to be signalled -- dying of SIGINT, returning 130 on SIGTERM --
//	              so a gate can interrupt a recipe while that call is in progress
//
// A gate writes only the files its cases need; an absent file is the neutral
// behaviour, so the union above behaves for each gate exactly as its own
// purpose-built stub did.

// FakeDirEnv names the directory the fake reads its behaviour from and appends
// its call log to.
const FakeDirEnv = "FAKE_DOCKER_DIR"

// fakeNames are the executables this binary impersonates when PATH resolves one
// of them to it. nvidia-smi is present-but-failing on purpose: the integration
// recipe probes for a GPU, and without a deterministic answer the gate's verdict
// would depend on whether the runner happens to have one.
var fakeNames = map[string]bool{
	"docker":     true,
	"nvidia-smi": true,
}

// FakeInvocation reports the fake this process was invoked as, if any.
func FakeInvocation(argv0 string) (string, bool) {
	name := filepath.Base(argv0)
	return name, fakeNames[name]
}

// DispatchFake serves the invocation and exits when PATH resolved this binary as
// a faked executable, and returns otherwise.
//
// BOTH main AND TestMain MUST CALL IT, first thing, before any flag is parsed.
// When PATH resolved this binary as `docker` the arguments belong to Docker's
// grammar rather than the gate's, and `go test` never calls main — so a gate
// whose test drives a recipe end to end would have the recipe re-enter the whole
// test binary, once per docker call, without end. That is not a hypothetical:
// it is what the first draft of scripts/verifyteardown's test did.
func DispatchFake() {
	if name, isFake := FakeInvocation(os.Args[0]); isFake {
		os.Exit(RunFake(name, os.Args[1:], os.Stdout, os.Stderr))
	}
}

// HangFIFO is the FIFO, inside FakeDirEnv, on which a hung call announces
// itself. The gate creates it and reads it to learn the call is in progress.
const HangFIFO = "hang.fifo"

// hangInterruptedStatus is what a hung call returns once it receives SIGTERM:
// the status a shell reports for an interrupted command.
const hangInterruptedStatus = 130

// hang announces call on the FIFO and waits to be signalled.
//
// Only SIGTERM is caught. SIGINT is left to the Go runtime, which kills the
// process WITH that signal, as a real docker dies of a Ctrl-C. That matters:
// a shell waiting on a child that exited normally carries on with its script
// after a SIGINT, and one whose child died of SIGINT stops -- so a fake that
// exited 130 on SIGINT would let a recipe with no cleanup trap reach its
// explicit cleanup line and pass for the wrong reason.
func hang(dir, call string, stderr io.Writer) int {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)

	fifo := filepath.Join(dir, HangFIFO)
	info, err := os.Stat(fifo)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		fmt.Fprintln(stderr, "fake docker: hang.match is set but", fifo, "is not a FIFO")
		return ExitToolError
	}
	if err := appendLine(fifo, call); err != nil {
		fmt.Fprintln(stderr, "fake docker: cannot announce the hang:", err)
		return ExitToolError
	}
	<-signals
	return hangInterruptedStatus
}

// FakeDirUnsetError reports that the fake was invoked without the directory it
// reads its behaviour from. That means something other than the gate resolved
// this binary as docker, which must fail loudly rather than answer as a daemon.
type FakeDirUnsetError struct {
	Name string
}

func (e *FakeDirUnsetError) Error() string {
	return e.Name + " fake invoked without " + FakeDirEnv + " set"
}

// RunFake serves one invocation of the named fake executable and returns the
// status it exits with.
func RunFake(name string, args []string, stdout, stderr io.Writer) int {
	if name == "nvidia-smi" {
		// Present but non-functional, which is what a host without a GPU looks
		// like to the recipe's own probe.
		return 1
	}

	dir := os.Getenv(FakeDirEnv)
	if dir == "" {
		fmt.Fprintln(stderr, (&FakeDirUnsetError{Name: name}).Error())
		return ExitToolError
	}

	joined := strings.Join(args, " ")
	if err := appendLine(filepath.Join(dir, "calls.log"), joined); err != nil {
		fmt.Fprintln(stderr, "fake docker: cannot log the call:", err)
		return ExitToolError
	}

	if watched := readTrimmed(dir, "watch.path"); watched != "" {
		size := int64(-1)
		if info, err := os.Stat(watched); err == nil {
			size = info.Size()
		}
		if err := appendLine(filepath.Join(dir, "watch.log"), joined+"\t"+strconv.FormatInt(size, 10)); err != nil {
			fmt.Fprintln(stderr, "fake docker: cannot log the watched file:", err)
			return ExitToolError
		}
	}

	if match := readTrimmed(dir, "hang.match"); match != "" && strings.Contains(joined, match) {
		return hang(dir, joined, stderr)
	}

	if match := readTrimmed(dir, "fail.match"); match != "" && strings.Contains(joined, match) {
		fmt.Fprintf(stderr, "fake docker: refusing '%s'\n", joined)
		return 1
	}

	subcommand := ""
	if len(args) > 0 {
		subcommand = args[0]
	}
	switch subcommand {
	case "images":
		if out, err := os.ReadFile(filepath.Join(dir, "images.out")); err == nil {
			if _, werr := stdout.Write(out); werr != nil {
				fmt.Fprintln(stderr, "fake docker: cannot write the image list:", werr)
				return ExitToolError
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stderr, "fake docker: cannot read the image list:", err)
			return ExitToolError
		}
		return fakeImagesStatus(dir, stderr)
	case "rmi":
		failing := readLines(dir, "rmi.fail")
		for _, arg := range args {
			if failing[arg] {
				fmt.Fprintf(stderr, "Error response from daemon: conflict: unable to remove %s\n", arg)
				return 1
			}
		}
		return ExitClean
	default:
		return ExitClean
	}
}

// fakeImagesStatus is the status `docker images` exits with: whatever images.rc
// names, or 0. A file that is present but unreadable as a number is a fixture
// defect and fails the fake rather than defaulting to success — a daemon that
// answered 0 when the case meant it to fail is how the "unreachable daemon" case
// would silently stop testing anything.
func fakeImagesStatus(dir string, stderr io.Writer) int {
	raw, err := os.ReadFile(filepath.Join(dir, "images.rc"))
	if errors.Is(err, os.ErrNotExist) {
		return ExitClean
	}
	if err != nil {
		fmt.Fprintln(stderr, "fake docker: cannot read images.rc:", err)
		return ExitToolError
	}
	status, cerr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if cerr != nil {
		fmt.Fprintln(stderr, "fake docker: images.rc is not a status:", cerr)
		return ExitToolError
	}
	return status
}

// InstallFakes prepares binDir and points every faked executable name at this
// process's own binary, returning the directory to prepend to PATH.
func InstallFakes(binDir string) error {
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	for name := range fakeNames {
		link := filepath.Join(binDir, name)
		if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Symlink(self, link); err != nil {
			return err
		}
	}
	return nil
}

// appendLine adds one line to a log the fake shares with concurrent
// invocations, so the open is append-only and one write carries the whole line.
func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		if cerr := f.Close(); cerr != nil {
			return cerr
		}
		return err
	}
	return f.Close()
}

// readTrimmed returns the whitespace-trimmed contents of a control file, or "".
func readTrimmed(dir, name string) string {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// readLines returns a control file's non-empty lines as a set.
func readLines(dir, name string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line != "" {
			out[line] = true
		}
	}
	return out
}

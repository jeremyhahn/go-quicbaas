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

// Command verifycliflags diffs the `init` flag set of xkmsctl against quicbaas to
// catch operator-UX drift across the pair.
//
// The flag set is sourced from each binary's `init --help` output, which is the
// surface an operator actually meets. Long flag names are extracted and xkmsctl —
// the canonical reference — is compared against quicbaas. QuicBaaS may carry
// additional QuicBaaS-specific flags not present on xkmsctl, but must NOT be
// missing any xkmsctl flag; an xkmsctl flag quicbaas omits is drift.
//
// BOTH BINARIES ARE REQUIRED. The shell gate this replaces built each one with
// its errors discarded and exited 0 when either failed to build, so a compile
// error in xkmsctl read as a pass (go-quicbaas#942). A gate that cannot build its
// reference has not compared anything, and says so with a typed error naming the
// package that would not build.
//
// qrdbctl IS GONE, not dropped by accident: the shell gate built it, extracted
// its flag set into QRDB_FLAGS at verify-cli-flags.sh:50, and never read the
// variable again. It was a minute of build time spent on a comparison nobody
// made.
//
// EXIT: 0 no drift, or drift while advisory · 1 drift under -strict · 2 the gate
// could not run.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// referencePackage is the canonical operator surface; subjectPackage is the one
// held against it.
const (
	referencePackage = "github.com/jeremyhahn/go-xkms/cmd/xkmsctl"
	subjectPackage   = "github.com/jeremyhahn/go-quicbaas/cmd/quicbaas"
	subcommand       = "init"
)

// longFlag matches a long flag name as it appears in help output.
var longFlag = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// allowedDifferences are xkmsctl flags that legitimately differ in name or are
// intentionally absent from quicbaas — xkmsctl-specific operator workflows that
// QuicBaaS inherits through different verbs.
//
// The keys are flag NAMES, not secrets; gosec's G101 heuristic reads
// "--token" and "--user-pin" as credential material.
//
//nolint:gosec // G101: flag names, not credentials
var allowedDifferences = map[string]string{
	"--token":    "xkmsctl uses --token for legacy auth; quicbaas uses mTLS",
	"--user-pin": "xkmsctl PIN flow; QuicBaaS routes through the credential service",
	"--verbose":  "quicbaas uses --log-level",
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// BuildError reports that a binary the comparison needs would not build. The
// compiler's own output is carried, because "could not build" without it sends
// the reader back to reproduce what the gate already knows.
type BuildError struct {
	Package string
	Output  string
	Err     error
}

func (e *BuildError) Error() string {
	return "cannot build " + e.Package + ": " + e.Err.Error() + "\n" + e.Output
}

func (e *BuildError) Unwrap() error { return e.Err }

// EmptyFlagSetError reports that a binary's help output yielded no long flags.
// An empty reference set makes the comparison vacuously true, which is the shape
// of a gate that has stopped reading its input.
type EmptyFlagSetError struct {
	Package string
	// Output is what the binary printed, and Err why it stopped, if it did. A
	// CLI may legitimately answer --help non-zero, so neither is a failure on
	// its own -- but when the extraction is empty they are the only account of
	// what happened.
	Output string
	Err    error
}

func (e *EmptyFlagSetError) Error() string {
	msg := "extracted no long flags from " + e.Package + " " + subcommand + " --help"
	if e.Err != nil {
		msg += " (" + e.Err.Error() + ")"
	}
	if trimmed := strings.TrimSpace(e.Output); trimmed != "" {
		msg += ": " + trimmed
	}
	return msg
}

func (e *EmptyFlagSetError) Unwrap() error { return e.Err }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verifycliflags", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root to build from")
	strict := fs.Bool("strict", false, "fail on drift instead of reporting it as advisory")
	if err := fs.Parse(args); err != nil {
		return gate.ExitToolError
	}

	work, err := os.MkdirTemp("", "verifycliflags-")
	if err != nil {
		fmt.Fprintln(stderr, "verify-cli-flags:", err)
		return gate.ExitToolError
	}
	defer func() {
		if rerr := os.RemoveAll(work); rerr != nil {
			fmt.Fprintln(stderr, "verify-cli-flags: cannot remove the workspace:", rerr)
		}
	}()

	reference, err := flagSetOf(*root, work, "xkmsctl", referencePackage)
	if err != nil {
		fmt.Fprintln(stderr, "verify-cli-flags FAIL:", err)
		return gate.ExitToolError
	}
	subject, err := flagSetOf(*root, work, "quicbaas", subjectPackage)
	if err != nil {
		fmt.Fprintln(stderr, "verify-cli-flags FAIL:", err)
		return gate.ExitToolError
	}

	return decide(reference, subject, *strict, stdout)
}

// decide is the gate's verdict: every reference flag the subject omits, minus the
// documented differences.
func decide(reference, subject []string, strict bool, stdout io.Writer) int {
	missing := drift(reference, subject)
	if len(missing) == 0 {
		fmt.Fprintln(stdout, "verify-cli-flags OK: every xkmsctl init flag is present on quicbaas init")
		return gate.ExitClean
	}

	if strict {
		fmt.Fprintln(stdout, "verify-cli-flags FAIL (-strict): quicbaas init missing xkmsctl init flags:")
		for _, name := range missing {
			fmt.Fprintln(stdout, "  "+name)
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Add the missing flags to cmd/quicbaas/init.go or, if intentionally")
		fmt.Fprintln(stdout, "absent, append to allowedDifferences in scripts/verifycliflags/main.go.")
		return gate.ExitFindings
	}

	fmt.Fprintln(stdout, "verify-cli-flags WARN: quicbaas init missing xkmsctl init flags (advisory):")
	for _, name := range missing {
		fmt.Fprintln(stdout, "  "+name)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Several xkmsctl init flags are not yet on quicbaas init. The flag-set")
	fmt.Fprintln(stdout, "drift is tracked but not blocking — re-run with STRICT=1 to fail CI.")
	fmt.Fprintln(stdout, "Add the missing flags to cmd/quicbaas/init.go (preferred) or append")
	fmt.Fprintln(stdout, "allowedDifferences in scripts/verifycliflags/main.go (last resort).")
	return gate.ExitClean
}

// drift returns the reference flags the subject does not carry, excluding the
// documented differences, sorted.
func drift(reference, subject []string) []string {
	present := make(map[string]bool, len(subject))
	for _, name := range subject {
		present[name] = true
	}
	var missing []string
	for _, name := range reference {
		if present[name] {
			continue
		}
		if _, allowed := allowedDifferences[name]; allowed {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	return missing
}

// flagSetOf builds the package and returns the long flags its subcommand's help
// output advertises.
func flagSetOf(root, work, name, pkg string) ([]string, error) {
	binary := filepath.Join(work, name)
	build := exec.Command("go", "build", "-o", binary, pkg)
	build.Dir = root
	output, err := build.CombinedOutput()
	if err != nil {
		return nil, &BuildError{Package: pkg, Output: string(output), Err: err}
	}

	help := exec.Command(binary, subcommand, "--help")
	help.Dir = root
	// A CLI may answer --help with a non-zero status, so the status is not the
	// signal -- the text is. But if the text yields nothing, WHY the invocation
	// failed is the only thing that will tell the reader what happened, so it is
	// carried into the error rather than discarded.
	text, runErr := help.CombinedOutput()
	flags := extractFlags(string(text))
	if len(flags) == 0 {
		return nil, &EmptyFlagSetError{Package: pkg, Output: string(text), Err: runErr}
	}
	return flags, nil
}

// extractFlags returns the unique, sorted long flag names mentioned in help text.
func extractFlags(text string) []string {
	found := longFlag.FindAllString(text, -1)
	sort.Strings(found)
	out := found[:0]
	for i, name := range found {
		if i == 0 || found[i-1] != name {
			out = append(out, name)
		}
	}
	return out
}

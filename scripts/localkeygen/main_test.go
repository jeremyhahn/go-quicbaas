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

package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// analyze runs the scanner over one in-memory source file and returns the
// findings.
func analyze(t *testing.T, name, src string) []finding {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return scanFile(fset, f)
}

// callsOf returns the reported call names, so a test can assert on what was
// found without depending on line numbers.
func callsOf(findings []finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.call)
	}
	return out
}

// =============================================================================
// the cases the gate exists for
// =============================================================================

// Every forbidden call is found. This is go-quicbaas#910's exact inventory: six
// key generations across three packages plus the two x509 creation calls that
// accompanied them.
func TestScannerFindsEveryForbiddenCall(t *testing.T) {
	src := `package server

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
)

func generate() {
	_, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	_, _ = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	_, _ = rsa.GenerateKey(rand.Reader, 2048)
	_, _ = rsa.GenerateKey(rand.Reader, 4096)
	_, _, _ = ed25519.GenerateKey(rand.Reader)
	_, _ = x509.CreateCertificate(rand.Reader, nil, nil, nil, nil)
	_, _ = x509.CreateCertificateRequest(rand.Reader, nil, nil)
}
`
	found := analyze(t, "pkg/server/ca.go", src)
	if len(found) != 7 {
		t.Fatalf("found %d call(s) %v, want 7", len(found), callsOf(found))
	}
	want := map[string]int{
		"ecdsa.GenerateKey":             2,
		"rsa.GenerateKey":               2,
		"ed25519.GenerateKey":           1,
		"x509.CreateCertificate":        1,
		"x509.CreateCertificateRequest": 1,
	}
	got := map[string]int{}
	for _, call := range callsOf(found) {
		got[call]++
	}
	for call, n := range want {
		if got[call] != n {
			t.Errorf("%s reported %d time(s), want %d", call, got[call], n)
		}
	}
	for _, f := range found {
		if f.reason == "" {
			t.Errorf("%s reported with no explanation for the reader", f.call)
		}
	}
}

// Every finding carries the position of the call so the operator can go to it.
func TestScannerReportsThePosition(t *testing.T) {
	src := `package server

import (
	"crypto/ed25519"
	"crypto/rand"
)

func generate() {
	_, _, _ = ed25519.GenerateKey(rand.Reader)
}
`
	found := analyze(t, "pkg/signal/service.go", src)
	if len(found) != 1 {
		t.Fatalf("found %d call(s), want 1", len(found))
	}
	if found[0].pos.Filename != "pkg/signal/service.go" {
		t.Errorf("filename = %q", found[0].pos.Filename)
	}
	if found[0].pos.Line != 9 {
		t.Errorf("line = %d, want 9", found[0].pos.Line)
	}
}

// =============================================================================
// what must NOT be reported
// =============================================================================

// The sanctioned delegation targets are not findings: asking go-xkms for a key,
// signing a CSR through the CA, or calling the audited generator.
func TestScannerIgnoresDelegatedCalls(t *testing.T) {
	src := `package server

import (
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

func issue(ca xkmssdk.TrustedCA, backend keystore) {
	_, _, _ = dbxkms.GenerateRequesterKey("ecdsa-p256")
	_, _ = xkmssdk.CreateCSRWithKey(nil, nil)
	_, _ = ca.SignCSR(nil, nil, nil)
	_, _ = backend.GenerateECDSA(nil, nil)
	_, _ = xkmssdk.EncodePrivateKeyPEM(nil, 0, nil)
}
`
	if found := analyze(t, "pkg/server/ca.go", src); len(found) != 0 {
		t.Fatalf("delegated calls reported: %v", callsOf(found))
	}
}

// A method named GenerateKey on something that is not one of the named crypto
// packages is not a finding: the receiver has to be ecdsa, rsa, ed25519 or x509.
// Over-reporting here would push contributors toward suppressing the gate.
func TestScannerRequiresTheCryptoPackageReceiver(t *testing.T) {
	src := `package server

func run(kms keyManager, tls tlsHelper) {
	_, _ = kms.GenerateKey("id")
	_, _ = tls.CreateCertificate("host")
	_, _ = GenerateKey()
}
`
	if found := analyze(t, "pkg/server/kms.go", src); len(found) != 0 {
		t.Fatalf("non-crypto-package calls reported: %v", callsOf(found))
	}
}

// =============================================================================
// waivers
// =============================================================================

// A waiver with a reason silences the call it precedes. This is how pkg/signal's
// Signal-protocol key material stays in the tree without disabling the gate.
func TestScannerHonoursAReasonedWaiver(t *testing.T) {
	src := `package signal

import (
	"crypto/ed25519"
	"crypto/rand"
)

func register() {
	// local-keygen-ok: Signal-protocol identity key, not PKI.
	_, _, _ = ed25519.GenerateKey(rand.Reader)
}
`
	if found := analyze(t, "pkg/signal/service.go", src); len(found) != 0 {
		t.Fatalf("waived call reported: %v", callsOf(found))
	}
}

// A waiver with no reason is not a waiver. A bare marker would let the gate be
// silenced without anyone having to say why, which is the whole value of it.
func TestScannerRejectsAnEmptyWaiver(t *testing.T) {
	src := `package signal

import (
	"crypto/ed25519"
	"crypto/rand"
)

func register() {
	// local-keygen-ok:
	_, _, _ = ed25519.GenerateKey(rand.Reader)
}
`
	if found := analyze(t, "pkg/signal/service.go", src); len(found) != 1 {
		t.Fatalf("found %d call(s) %v, want the unreasoned waiver to be ignored", len(found), callsOf(found))
	}
}

// A waiver covers its own vicinity and not the whole function: one justified
// call must not silence an unjustified one further down.
func TestScannerDoesNotWaiveTheWholeFunction(t *testing.T) {
	src := `package signal

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
)

func register() {
	// local-keygen-ok: Signal-protocol identity key, not PKI.
	_, _, _ = ed25519.GenerateKey(rand.Reader)

	// eight
	// lines
	// of
	// unrelated
	// commentary
	// separating
	// the two
	_, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}
`
	found := analyze(t, "pkg/signal/service.go", src)
	if len(found) != 1 {
		t.Fatalf("found %d call(s) %v, want only the unwaived ecdsa call", len(found), callsOf(found))
	}
	if found[0].call != "ecdsa.GenerateKey" {
		t.Fatalf("reported %s, want ecdsa.GenerateKey", found[0].call)
	}
}

// =============================================================================
// collect
// =============================================================================

// Test files are out of scope by default and reported with -include-tests: a
// fixture minting a throwaway certificate is not a key-custody decision, but an
// audit should still be able to see them.
func TestCollectExcludesTestFilesUnlessAsked(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"prod.go", "prod_test.go", "notes.md"} {
		if err := os.WriteFile(filepath.Join(pkgDir, name), []byte("package p\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	production, err := collect([]string{pkgDir}, nil, false)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(production) != 1 || filepath.Base(production[0]) != "prod.go" {
		t.Fatalf("production scan = %v, want just prod.go", production)
	}

	withTests, err := collect([]string{pkgDir}, nil, true)
	if err != nil {
		t.Fatalf("collect with tests: %v", err)
	}
	if len(withTests) != 2 {
		t.Fatalf("scan with tests = %v, want both .go files", withTests)
	}
}

// Skipped prefixes and vendored/testdata directories are pruned, and a directory
// that does not exist is not an error — the scan runs on partial checkouts.
func TestCollectPrunesSkippedAndMissingPaths(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"pkg", "pkg/generated", "pkg/testdata", "vendor"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "f.go"), []byte("package p\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := collect(
		[]string{filepath.Join(dir, "pkg"), filepath.Join(dir, "vendor"), filepath.Join(dir, "absent")},
		[]string{filepath.Join(dir, "pkg", "generated")},
		false,
	)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(got) != 1 || got[0] != filepath.Join(dir, "pkg", "f.go") {
		t.Fatalf("collect = %v, want only pkg/f.go", got)
	}
}

// skipped matches a directory and everything under it, and nothing that merely
// shares a name prefix.
func TestSkipped(t *testing.T) {
	skips := []string{"pkg/generated"}
	tests := []struct {
		path string
		want bool
	}{
		{"pkg/generated", true},
		{"pkg/generated/a.go", true},
		{"pkg/generated/deep/a.go", true},
		{"pkg/generatedthing/a.go", false},
		{"pkg/server/a.go", false},
	}
	for _, tt := range tests {
		if got := skipped(tt.path, skips); got != tt.want {
			t.Errorf("skipped(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// An empty skip list excludes nothing.
func TestSkippedWithNoSkips(t *testing.T) {
	if skipped("pkg/server/a.go", nil) {
		t.Error("skipped reported true with no skip list")
	}
}

// =============================================================================
// the repeatable flag
// =============================================================================

// stringList accumulates across occurrences and renders them back for -h.
func TestStringListAccumulates(t *testing.T) {
	var list stringList
	for _, v := range []string{"pkg", "cmd", "sdk"} {
		if err := list.Set(v); err != nil {
			t.Fatalf("Set(%q): %v", v, err)
		}
	}
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}
	if got := list.String(); got != "pkg,cmd,sdk" {
		t.Fatalf("String() = %q", got)
	}
}

// A zero stringList renders as the empty string rather than panicking.
func TestStringListZeroValue(t *testing.T) {
	var list stringList
	if got := list.String(); got != "" {
		t.Fatalf("String() = %q, want empty", got)
	}
}

// =============================================================================
// the command itself
// =============================================================================

// writeTree materialises a small source tree under t.TempDir and returns its
// root, so a command-level test exercises the real filesystem walk rather than a
// stand-in for it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// A clean tree exits zero and says nothing. Silence on success matters: this
// runs on every `make test`, so anything it prints is printed thousands of
// times.
func TestRunReportsNothingForACleanTree(t *testing.T) {
	root := writeTree(t, map[string]string{
		"pkg/clean/clean.go": `package clean

import dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"

func New() { _, _, _ = dbxkms.GenerateRequesterKey("ecdsa-p256") }
`,
	})

	var stdout, stderr strings.Builder
	if code := run([]string{"-dir", root}, &stdout, &stderr); code != exitClean {
		t.Fatalf("exit code = %d, want %d (stdout %q stderr %q)", code, exitClean, stdout.String(), stderr.String())
	}
	if stdout.String() != "" {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

// A violating tree exits non-zero, names the file and the call on stdout, and
// puts the remediation on stderr. The remediation text is asserted because the
// gate's whole value is that whoever trips it learns what to do instead.
func TestRunFailsAndExplainsAViolation(t *testing.T) {
	root := writeTree(t, map[string]string{
		"pkg/bad/bad.go": `package bad

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
)

func Bad() { _, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader) }
`,
	})

	var stdout, stderr strings.Builder
	if code := run([]string{"-dir", root}, &stdout, &stderr); code != exitFindings {
		t.Fatalf("exit code = %d, want %d", code, exitFindings)
	}
	if !strings.Contains(stdout.String(), "ecdsa.GenerateKey") {
		t.Errorf("stdout does not name the call: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), filepath.Join("pkg", "bad", "bad.go")) {
		t.Errorf("stdout does not name the file: %q", stdout.String())
	}
	for _, want := range []string{"1 site(s)", "GenerateRequesterKey", "TrustedCA.SignCSR", waiverPrefix} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr is missing %q: %q", want, stderr.String())
		}
	}
}

// A waived site in an otherwise violating tree passes, so the escape hatch the
// error message advertises actually works end to end.
func TestRunHonoursAWaiverEndToEnd(t *testing.T) {
	root := writeTree(t, map[string]string{
		"pkg/waived/waived.go": `package waived

import (
	"crypto/ed25519"
	"crypto/rand"
)

// local-keygen-ok: Signal-protocol identity key, not PKI.
func Identity() { _, _, _ = ed25519.GenerateKey(rand.Reader) }
`,
	})

	var stdout, stderr strings.Builder
	if code := run([]string{"-dir", root}, &stdout, &stderr); code != exitClean {
		t.Fatalf("exit code = %d, want %d (stdout %q)", code, exitClean, stdout.String())
	}
}

// An unparseable flag is a tool error, not a finding: CI must be able to tell
// "the tree is dirty" from "the gate was invoked wrongly".
func TestRunRejectsAnUnknownFlag(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := run([]string{"-nope"}, &stdout, &stderr); code != exitToolError {
		t.Fatalf("exit code = %d, want %d", code, exitToolError)
	}
	if stdout.String() != "" {
		t.Errorf("stdout = %q, want empty on a usage error", stdout.String())
	}
}

// A -dir that is a file rather than a directory fails the walk, which must also
// surface as a tool error with the cause named.
func TestRunReportsAWalkFailure(t *testing.T) {
	root := t.TempDir()
	unreadable := filepath.Join(root, "locked")
	if err := os.MkdirAll(filepath.Join(unreadable, "inner"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	var stdout, stderr strings.Builder
	code := run([]string{"-dir", unreadable}, &stdout, &stderr)
	if os.Geteuid() == 0 {
		// root ignores the mode bits, so there is nothing to fail on.
		if code != exitClean {
			t.Fatalf("as root: exit code = %d, want %d", code, exitClean)
		}
		return
	}
	if code != exitToolError {
		t.Fatalf("exit code = %d, want %d (stderr %q)", code, exitToolError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "localkeygen:") {
		t.Errorf("stderr does not name the tool: %q", stderr.String())
	}
}

// scanFiles orders findings by file then offset and tolerates a file that does
// not parse, because the gate runs on trees mid-edit and must not mask a
// compiler error with one of its own.
func TestScanFilesOrdersAndSkipsUnparseableFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"b.go": `package p

import (
	"crypto/rsa"
	"crypto/rand"
)

func B() { _, _ = rsa.GenerateKey(rand.Reader, 2048) }
`,
		"a.go": `package p

import (
	"crypto/ed25519"
	"crypto/rand"
)

func A1() { _, _, _ = ed25519.GenerateKey(rand.Reader) }
func A2() { _, _, _ = ed25519.GenerateKey(rand.Reader) }
`,
		"broken.go": "package p\nfunc ( {",
	})

	findings := scanFiles([]string{
		filepath.Join(root, "b.go"),
		filepath.Join(root, "broken.go"),
		filepath.Join(root, "a.go"),
	})

	got := callsOf(findings)
	want := []string{"ed25519.GenerateKey", "ed25519.GenerateKey", "rsa.GenerateKey"}
	if len(got) != len(want) {
		t.Fatalf("findings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("findings = %v, want %v", got, want)
		}
	}
	if findings[0].pos.Line > findings[1].pos.Line {
		t.Errorf("findings within a file are not offset-ordered: %d then %d", findings[0].pos.Line, findings[1].pos.Line)
	}
}

// scanFiles over nothing is not an error and reports nothing.
func TestScanFilesWithNoFiles(t *testing.T) {
	if findings := scanFiles(nil); len(findings) != 0 {
		t.Fatalf("findings = %v, want none", callsOf(findings))
	}
}

// =============================================================================
// scope: the test tree is in it (go-quicbaas#943)
// =============================================================================

// The default scan set names the test tree.
//
// This is asserted on the data rather than on a run because it is the fact the
// regression turned on: test/ was excluded, and a 444-line parallel certificate
// authority grew there and became the source of the entire integration suite's
// TLS material. A change that drops "test" from the default set silently
// restores that blind spot, so it fails here.
func TestDefaultDirsCoverTheTestTree(t *testing.T) {
	want := []string{"pkg", "cmd", "sdk", "test"}
	got := defaultDirs()
	if len(got) != len(want) {
		t.Fatalf("defaultDirs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("defaultDirs() = %v, want %v", got, want)
		}
	}
}

// A new bespoke certificate generator under test/ fails the gate.
//
// This is the regression test for go-quicbaas#943 as a whole: the defect was
// not that a particular file existed, it was that nothing in the build objected
// to one existing under test/. The tree here is the shape of what was found —
// a package under test/integration that builds its own key and signs its own
// certificate — and the gate must name both calls and both point at the file.
func TestRunFailsOnANewBespokeGeneratorUnderTest(t *testing.T) {
	root := writeTree(t, map[string]string{
		"test/integration/gen-certs/main.go": `package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
)

func generateCA() {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{}
	_, _ = x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
}
`,
	})

	var stdout, stderr strings.Builder
	if code := run([]string{"-dir", filepath.Join(root, "test")}, &stdout, &stderr); code != exitFindings {
		t.Fatalf("exit code = %d, want %d (stdout %q)", code, exitFindings, stdout.String())
	}
	for _, want := range []string{"ecdsa.GenerateKey", "x509.CreateCertificate", "gen-certs"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout is missing %q: %q", want, stdout.String())
		}
	}
	if !strings.Contains(stderr.String(), "2 site(s)") {
		t.Errorf("stderr does not count both sites: %q", stderr.String())
	}
}

// The integration fixture path this issue moved to passes the gate: it asks
// go-xkms for the key and hands the CSR to the CA, so there is nothing to
// report. Without this case the test above would pass for a gate that failed
// everything under test/, which would be just as useless.
func TestRunPassesTheProductIssuancePathUnderTest(t *testing.T) {
	root := writeTree(t, map[string]string{
		"test/integration/certfixture/authority.go": `package certfixture

import (
	"context"

	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

func issue(ctx context.Context, ca xkmssdk.TrustedCA) {
	signer, _, _ := dbxkms.GenerateRequesterKey("ecdsa-p256")
	csr, _ := xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{}, signer)
	_, _ = ca.SignCSR(ctx, csr, nil)
}
`,
	})

	var stdout, stderr strings.Builder
	if code := run([]string{"-dir", filepath.Join(root, "test")}, &stdout, &stderr); code != exitClean {
		t.Fatalf("exit code = %d, want %d (stdout %q stderr %q)", code, exitClean, stdout.String(), stderr.String())
	}
}

// A _test.go file under test/ is still out of scope by default, and in scope
// with -include-tests. The exclusion is uniform across every tree: a unit test's
// self-signed throwaway chains to nothing and decides no key custody. It is
// asserted so that widening the tree scope is not mistaken for widening the file
// scope, and so the audit flag keeps working.
func TestRunExcludesTestFilesUnderTestUnlessAsked(t *testing.T) {
	root := writeTree(t, map[string]string{
		"test/integration/iam/dpop_test.go": `package iam

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
)

func fixture() { _, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader) }
`,
	})
	dir := filepath.Join(root, "test")

	var stdout, stderr strings.Builder
	if code := run([]string{"-dir", dir}, &stdout, &stderr); code != exitClean {
		t.Fatalf("default scope: exit code = %d, want %d (stdout %q)", code, exitClean, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-dir", dir, "-include-tests"}, &stdout, &stderr); code != exitFindings {
		t.Fatalf("-include-tests: exit code = %d, want %d", code, exitFindings)
	}
	if !strings.Contains(stdout.String(), "dpop_test.go") {
		t.Errorf("-include-tests did not report the test file: %q", stdout.String())
	}
}

// An explicit -skip exempts a named path under test/, which is the documented
// replacement for the blanket tree exclusion: an exemption is a listed decision
// with a path attached, not a whole tree nobody looks at.
func TestRunHonoursAnExplicitSkipUnderTest(t *testing.T) {
	root := writeTree(t, map[string]string{
		"test/integration/legacy/legacy.go": `package legacy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
)

func Bad() { _, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader) }
`,
	})
	dir := filepath.Join(root, "test")

	var stdout, stderr strings.Builder
	if code := run([]string{"-dir", dir}, &stdout, &stderr); code != exitFindings {
		t.Fatalf("without -skip: exit code = %d, want %d", code, exitFindings)
	}

	stdout.Reset()
	stderr.Reset()
	skip := filepath.Join(dir, "integration", "legacy")
	if code := run([]string{"-dir", dir, "-skip", skip}, &stdout, &stderr); code != exitClean {
		t.Fatalf("with -skip %s: exit code = %d, want %d (stdout %q)", skip, code, exitClean, stdout.String())
	}
}

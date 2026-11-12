package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testNow is the injected current time every test checks against.
var testNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

// listingWithStaleRelease is testListing with example.com/stale's latest
// release published at released.
func listingWithStaleRelease(released time.Time) string {
	return strings.Replace(testListing, `"Update":{"Version":"v1.4.0"}`,
		`"Update":{"Version":"v1.4.0","Time":"`+released.Format(time.RFC3339)+`"}`, 1)
}

const testModFile = `module example.com/app

go 1.27

require (
	example.com/current v1.2.0
	example.com/stale v1.0.0
	// Held: v2 drops the API this repository calls (upstream#7).
	example.com/held v1.0.0
	example.com/suffixed v1.0.0 // held for upstream#9
	example.com/replaced v1.0.0
	example.com/local v0.0.0
	example.com/transitive v1.0.0 // indirect
)

// Held at v1.0.0 until upstream#11 ships.
replace example.com/replaced => example.com/replaced v1.0.0

replace example.com/local => ./local
`

const testListing = `
{"Path":"example.com/app","Main":true}
{"Path":"example.com/current","Version":"v1.2.0"}
{"Path":"example.com/stale","Version":"v1.0.0","Update":{"Version":"v1.4.0"}}
{"Path":"example.com/held","Version":"v1.0.0","Update":{"Version":"v2.0.0"}}
{"Path":"example.com/suffixed","Version":"v1.0.0","Update":{"Version":"v1.1.0"}}
{"Path":"example.com/replaced","Version":"v1.0.0","Update":{"Version":"v1.3.0"}}
{"Path":"example.com/local","Version":"v0.0.0","Update":{"Version":"v0.1.0"},"Replace":{"Path":"./local"}}
{"Path":"example.com/transitive","Version":"v1.0.0","Update":{"Version":"v1.9.0"}}
`

func TestCheck_ReportsOnlyUndocumentedDirectRequirements(t *testing.T) {
	reported, err := Check("go.mod", []byte(testModFile), strings.NewReader(testListing), testNow)
	if len(reported) != 0 {
		t.Errorf("reported = %v, want none: a release with no time is old", reported)
	}

	var behindErr *BehindError
	if !errors.As(err, &behindErr) {
		t.Fatalf("Check() error = %v, want *BehindError", err)
	}
	want := []Behind{{Path: "example.com/stale", Current: "v1.0.0", Latest: "v1.4.0"}}
	if len(behindErr.Behind) != len(want) || behindErr.Behind[0] != want[0] {
		t.Fatalf("Behind = %v, want %v", behindErr.Behind, want)
	}
	if !strings.Contains(err.Error(), "example.com/stale v1.0.0 -> v1.4.0") {
		t.Errorf("error %q does not name the stale module and its versions", err)
	}
}

func TestCheck_PassesWhenEveryRequirementIsCurrentOrDocumented(t *testing.T) {
	listing := strings.Replace(testListing, `"Update":{"Version":"v1.4.0"}`, `"Update":null`, 1)

	reported, err := Check("go.mod", []byte(testModFile), strings.NewReader(listing), testNow)
	if err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
	if len(reported) != 0 {
		t.Errorf("reported = %v, want none", reported)
	}
}

func TestCheck_FailsClosedOnUnresolvedModule(t *testing.T) {
	listing := strings.Replace(testListing,
		`"Update":{"Version":"v1.4.0"}`, `"Error":{"Err":"terminal prompts disabled"}`, 1)

	_, err := Check("go.mod", []byte(testModFile), strings.NewReader(listing), testNow)

	var unresolvedErr *UnresolvedError
	if !errors.As(err, &unresolvedErr) {
		t.Fatalf("Check() error = %v, want *UnresolvedError", err)
	}
	if len(unresolvedErr.Modules) != 1 || !strings.HasPrefix(unresolvedErr.Modules[0], "example.com/stale: ") {
		t.Errorf("Modules = %v, want only example.com/stale", unresolvedErr.Modules)
	}
	var behindErr *BehindError
	if errors.As(err, &behindErr) {
		t.Errorf("Check() reported %v as behind, want only unresolved", behindErr.Behind)
	}
}

func TestCheck_RejectsUnparsableModFile(t *testing.T) {
	_, err := Check("go.mod", []byte("require (\n"), strings.NewReader(testListing), testNow)

	var modErr *ModFileError
	if !errors.As(err, &modErr) {
		t.Fatalf("Check() error = %v, want *ModFileError", err)
	}
}

func TestCheck_RejectsMalformedListing(t *testing.T) {
	_, err := Check("go.mod", []byte(testModFile), strings.NewReader(`{"Path":`), testNow)

	var listErr *ModuleListError
	if !errors.As(err, &listErr) {
		t.Fatalf("Check() error = %v, want *ModuleListError", err)
	}
}

func TestCheck_ReportsReleaseYoungerThanWindow(t *testing.T) {
	released := testNow.Add(-2 * time.Hour)

	reported, err := Check("go.mod", []byte(testModFile), strings.NewReader(listingWithStaleRelease(released)), testNow)
	if err != nil {
		t.Fatalf("Check() error = %v, want nil for a release 2h old", err)
	}
	want := Behind{Path: "example.com/stale", Current: "v1.0.0", Latest: "v1.4.0", Released: released}
	if len(reported) != 1 || !reported[0].Released.Equal(want.Released) || reported[0].String() != want.String() {
		t.Fatalf("reported = %v, want [%v]", reported, want)
	}
}

func TestCheck_FailsReleaseOlderThanWindow(t *testing.T) {
	released := testNow.Add(-RecentReleaseWindow - time.Hour)

	reported, err := Check("go.mod", []byte(testModFile), strings.NewReader(listingWithStaleRelease(released)), testNow)
	var behindErr *BehindError
	if !errors.As(err, &behindErr) {
		t.Fatalf("Check() error = %v, want *BehindError for a release 7d1h old", err)
	}
	if len(behindErr.Behind) != 1 || behindErr.Behind[0].Path != "example.com/stale" {
		t.Errorf("Behind = %v, want only example.com/stale", behindErr.Behind)
	}
	if len(reported) != 0 {
		t.Errorf("reported = %v, want none", reported)
	}
}

func TestCheck_FailsReleaseExactlyAtWindow(t *testing.T) {
	released := testNow.Add(-RecentReleaseWindow)

	reported, err := Check("go.mod", []byte(testModFile), strings.NewReader(listingWithStaleRelease(released)), testNow)
	var behindErr *BehindError
	if !errors.As(err, &behindErr) {
		t.Fatalf("Check() error = %v, want *BehindError: a release exactly 7 days old fails", err)
	}
	if len(reported) != 0 {
		t.Errorf("reported = %v, want none", reported)
	}
}

func TestCheck_ReportsReleaseJustInsideWindow(t *testing.T) {
	released := testNow.Add(-RecentReleaseWindow + time.Second)

	reported, err := Check("go.mod", []byte(testModFile), strings.NewReader(listingWithStaleRelease(released)), testNow)
	if err != nil {
		t.Fatalf("Check() error = %v, want nil for a release 1s under 7 days old", err)
	}
	if len(reported) != 1 {
		t.Errorf("reported = %v, want example.com/stale", reported)
	}
}

func TestCheck_PassesOldReleaseWithHeldReason(t *testing.T) {
	modFile := strings.Replace(testModFile, "\texample.com/stale v1.0.0\n",
		"\texample.com/stale v1.0.0 // held: upstream#12, v1.4 changes the wire format\n", 1)
	released := testNow.Add(-30 * 24 * time.Hour)

	reported, err := Check("go.mod", []byte(modFile), strings.NewReader(listingWithStaleRelease(released)), testNow)
	if err != nil {
		t.Fatalf("Check() error = %v, want nil for a held requirement", err)
	}
	if len(reported) != 0 {
		t.Errorf("reported = %v, want none: a held requirement is neither failed nor reported", reported)
	}
}

func TestCheck_FailsReleaseWithNoTime(t *testing.T) {
	reported, err := Check("go.mod", []byte(testModFile), strings.NewReader(testListing), testNow)

	var behindErr *BehindError
	if !errors.As(err, &behindErr) {
		t.Fatalf("Check() error = %v, want *BehindError: a release with no time is treated as old", err)
	}
	if !behindErr.Behind[0].Released.IsZero() {
		t.Errorf("Released = %v, want zero", behindErr.Behind[0].Released)
	}
	if len(reported) != 0 {
		t.Errorf("reported = %v, want none", reported)
	}
}

func TestCheck_SplitsYoungAndOldReleases(t *testing.T) {
	listing := listingWithStaleRelease(testNow.Add(-8 * 24 * time.Hour))
	listing = strings.Replace(listing, `{"Path":"example.com/current","Version":"v1.2.0"}`,
		`{"Path":"example.com/current","Version":"v1.2.0","Update":{"Version":"v1.3.0","Time":"`+
			testNow.Add(-time.Hour).Format(time.RFC3339)+`"}}`, 1)

	reported, err := Check("go.mod", []byte(testModFile), strings.NewReader(listing), testNow)
	var behindErr *BehindError
	if !errors.As(err, &behindErr) {
		t.Fatalf("Check() error = %v, want *BehindError", err)
	}
	if len(behindErr.Behind) != 1 || behindErr.Behind[0].Path != "example.com/stale" {
		t.Errorf("Behind = %v, want only example.com/stale", behindErr.Behind)
	}
	if len(reported) != 1 || reported[0].Path != "example.com/current" {
		t.Errorf("reported = %v, want only example.com/current", reported)
	}
}

func TestBehind_ReportedLineNamesVersionsAndAge(t *testing.T) {
	b := Behind{Path: "example.com/stale", Current: "v1.0.0", Latest: "v1.4.0", Released: testNow.Add(-26 * time.Hour)}

	got := b.ReportedLine(testNow)
	for _, want := range []string{"example.com/stale v1.0.0 -> v1.4.0", "2026-10-24T10:00:00Z", "26h0m0s ago", "under 7 days"} {
		if !strings.Contains(got, want) {
			t.Errorf("ReportedLine() = %q, want it to contain %q", got, want)
		}
	}
}

// writeModFile writes testModFile to a temporary go.mod and returns its path.
func writeModFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go.mod")
	if err := os.WriteFile(path, []byte(testModFile), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return path
}

func TestRun_ExitsZeroWhenOnlyReportedEntriesExist(t *testing.T) {
	modPath := writeModFile(t)
	listing := listingWithStaleRelease(testNow.Add(-3 * time.Hour))
	var stdout, stderr bytes.Buffer

	code := run([]string{"depscheck", modPath}, strings.NewReader(listing), &stdout, &stderr, testNow)
	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "example.com/stale v1.0.0 -> v1.4.0") || !strings.Contains(stdout.String(), "3h0m0s ago") {
		t.Errorf("stdout = %q, want the reported requirement with its versions and age", stdout.String())
	}
}

func TestRun_ExitsOneOnAnOldRelease(t *testing.T) {
	modPath := writeModFile(t)
	listing := listingWithStaleRelease(testNow.Add(-10 * 24 * time.Hour))
	var stdout, stderr bytes.Buffer

	code := run([]string{"depscheck", modPath}, strings.NewReader(listing), &stdout, &stderr, testNow)
	if code != 1 {
		t.Fatalf("run() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "example.com/stale v1.0.0 -> v1.4.0") {
		t.Errorf("stderr = %q, want the failing requirement", stderr.String())
	}
}

func TestRun_RejectsBadUsageAndMissingModFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"depscheck"}, strings.NewReader(""), &stdout, &stderr, testNow); code != 2 {
		t.Errorf("run() with no go.mod argument = %d, want 2", code)
	}
	missing := filepath.Join(t.TempDir(), "absent.mod")
	if code := run([]string{"depscheck", missing}, strings.NewReader(""), &stdout, &stderr, testNow); code != 1 {
		t.Errorf("run() with a missing go.mod = %d, want 1", code)
	}
}

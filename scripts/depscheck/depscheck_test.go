package main

import (
	"errors"
	"strings"
	"testing"
)

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
	err := Check("go.mod", []byte(testModFile), strings.NewReader(testListing))

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

	if err := Check("go.mod", []byte(testModFile), strings.NewReader(listing)); err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
}

func TestCheck_FailsClosedOnUnresolvedModule(t *testing.T) {
	listing := strings.Replace(testListing,
		`"Update":{"Version":"v1.4.0"}`, `"Error":{"Err":"terminal prompts disabled"}`, 1)

	err := Check("go.mod", []byte(testModFile), strings.NewReader(listing))

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
	err := Check("go.mod", []byte("require (\n"), strings.NewReader(testListing))

	var modErr *ModFileError
	if !errors.As(err, &modErr) {
		t.Fatalf("Check() error = %v, want *ModFileError", err)
	}
}

func TestCheck_RejectsMalformedListing(t *testing.T) {
	err := Check("go.mod", []byte(testModFile), strings.NewReader(`{"Path":`))

	var listErr *ModuleListError
	if !errors.As(err, &listErr) {
		t.Fatalf("Check() error = %v, want *ModuleListError", err)
	}
}

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

package buildgates

import (
	"path/filepath"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// composedLibraries are the libraries go-quicbaas composes (SPEC row
// composition, #1648). Each must be pinned to a released tag: a
// pseudo-version pins an untagged commit, which no release of the library
// vouches for.
var composedLibraries = []string{
	"github.com/jeremyhahn/go-qrdb",
	"github.com/jeremyhahn/go-qrdb/sdk/go",
	"github.com/jeremyhahn/go-xkms",
}

// pinProblem reports why version is not an acceptable pin for a composed
// library, or "" when it is a released tag.
func pinProblem(version string) string {
	switch {
	case version == "":
		return "not required"
	case !semver.IsValid(version):
		return "not a semantic version"
	case module.IsPseudoVersion(version):
		return "a pseudo-version, not a released tag"
	case semver.Build(version) != "":
		return "carries build metadata"
	}
	return ""
}

// pinsOf returns the required version of each of libs in the go.mod text
// gomod, after replace directives: a replace to a local path is reported as
// "replaced by <path>", and a replace to another version is the pin.
func pinsOf(t *testing.T, gomod []byte, libs []string) map[string]string {
	t.Helper()
	f, err := modfile.Parse("go.mod", gomod, nil)
	if err != nil {
		t.Fatalf("parse go.mod: %v", err)
	}
	pins := make(map[string]string, len(libs))
	for _, lib := range libs {
		pins[lib] = ""
		for _, r := range f.Require {
			if r.Mod.Path == lib {
				pins[lib] = r.Mod.Version
			}
		}
		for _, r := range f.Replace {
			if r.Old.Path != lib || (r.Old.Version != "" && r.Old.Version != pins[lib]) {
				continue
			}
			if r.New.Version == "" {
				pins[lib] = "replaced by " + r.New.Path
			} else {
				pins[lib] = r.New.Version
			}
		}
	}
	return pins
}

func TestComposedLibrariesArePinnedToReleasedTags(t *testing.T) {
	gomod := readFile(t, filepath.Join(repoRoot, "go.mod"))
	for lib, version := range pinsOf(t, []byte(gomod), composedLibraries) {
		if problem := pinProblem(version); problem != "" {
			t.Errorf("go.mod pins %s at %q: %s", lib, version, problem)
		}
	}
}

func TestPinGateRefusesWhatIsNotAReleasedTag(t *testing.T) {
	gomod := []byte(`module example.com/m

go 1.24

require (
	github.com/jeremyhahn/go-qrdb v0.30.2-0.20261101120000-0123456789ab
	github.com/jeremyhahn/go-xkms v0.49.1-alpha
)

replace github.com/jeremyhahn/go-xkms => ../go-xkms
`)
	pins := pinsOf(t, gomod, composedLibraries)
	for lib, want := range map[string]string{
		"github.com/jeremyhahn/go-qrdb":        "a pseudo-version, not a released tag",
		"github.com/jeremyhahn/go-qrdb/sdk/go": "not required",
		"github.com/jeremyhahn/go-xkms":        "not a semantic version",
	} {
		if got := pinProblem(pins[lib]); got != want {
			t.Errorf("%s pinned at %q: problem %q, want %q", lib, pins[lib], got, want)
		}
	}
	for _, tag := range []string{"v0.30.1-alpha", "v0.58.2", "v1.0.0"} {
		if got := pinProblem(tag); got != "" {
			t.Errorf("released tag %s refused: %s", tag, got)
		}
	}
}

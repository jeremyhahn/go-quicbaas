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

package softwarefactoryworker

import (
	"slices"
	"strings"
	"testing"
)

// baseStageFrom opens the image's final stage, whose first `apt-get install`
// is the list of packages the image carries.
const baseStageFrom = "FROM debian:trixie-slim"

// baseAptPackages returns the package names the final stage's first
// `apt-get install` names, one per continuation line, or nil when the
// Dockerfile has no such stage or install.
func baseAptPackages(dockerfile string) []string {
	_, stage, found := strings.Cut(dockerfile, baseStageFrom)
	if !found {
		return nil
	}
	_, list, found := strings.Cut(stage, "apt-get install -y --no-install-recommends \\\n")
	if !found {
		return nil
	}
	var packages []string
	for line := range strings.Lines(list) {
		line = strings.TrimSpace(line)
		if name, last := strings.CutSuffix(line, "; \\"); last {
			return append(packages, name)
		}
		packages = append(packages, strings.TrimSpace(strings.TrimSuffix(line, "\\")))
	}
	return nil
}

// TestDockerfile_BaseImageCarriesJq proves jq is in the image's package list:
// release scripts run in a worker need it, starting with go-enclave's
// release.sh (go-quicbaas#1431).
func TestDockerfile_BaseImageCarriesJq(t *testing.T) {
	packages := baseAptPackages(string(Dockerfile))
	if len(packages) == 0 {
		t.Fatal("the Dockerfile's base apt-get install list was not found")
	}
	if !slices.Contains(packages, "jq") {
		t.Errorf("the image's apt list %v does not carry jq", packages)
	}
	if !strings.Contains(string(Dockerfile), "\nRUN jq --version\n") {
		t.Error("the Dockerfile does not assert jq --version after the sudo proof")
	}
}

func TestDockerfile_BaseImageCarriesGcovr(t *testing.T) {
	packages := baseAptPackages(string(Dockerfile))
	if len(packages) == 0 {
		t.Fatal("the Dockerfile's base apt-get install list was not found")
	}
	if !slices.Contains(packages, "gcovr") {
		t.Errorf("the image's apt list %v does not carry gcovr", packages)
	}
	if !strings.Contains(string(Dockerfile), "\nRUN gcovr --version\n") {
		t.Error("the Dockerfile does not assert gcovr --version after the sudo proof")
	}
}

// TestDockerfile_BaseImageCarriesTheJDK proves the runtime stage installs the
// headless JDK at the pinned major (OPENJDK_MAJOR), from Debian's archive
// beside the other apt packages, rather than a hard-coded major the pin and
// its label could drift from.
func TestDockerfile_BaseImageCarriesTheJDK(t *testing.T) {
	packages := baseAptPackages(string(Dockerfile))
	if !slices.Contains(packages, `"openjdk-${OPENJDK_MAJOR}-jdk-headless"`) {
		t.Errorf("the image's apt list %v does not install the pinned headless JDK", packages)
	}
	if slices.Contains(packages, "openjdk-21-jdk-headless") {
		t.Error("the JDK major must come from OPENJDK_MAJOR, not be written into the apt list")
	}
}

// TestProbes_ToolchainChecksJavaAndKotlinLsp proves the toolchain probe fails
// an image whose java is not the pinned major, or whose kotlin-lsp is not on
// the login PATH: the two tools #1746 adds are certified like every other.
func TestProbes_ToolchainChecksJavaAndKotlinLsp(t *testing.T) {
	var script string
	for _, p := range Probes() {
		if p.Name == "toolchain, signing and every agent" {
			script = p.Script
		}
	}
	if script == "" {
		t.Fatal("the toolchain probe was not found")
	}
	for _, want := range []string{
		`java -version 2>&1 | head -1 | grep -qF "\"$EXPECTED_OPENJDK_MAJOR."`,
		"command -v kotlin-lsp",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the toolchain probe does not run %q", want)
		}
	}
}

// TestInstallProof_PackageIsNotInTheImage proves both install proofs install a
// package the image does not carry: an install of a package already present
// installs nothing and proves nothing (go-quicbaas#1431).
func TestInstallProof_PackageIsNotInTheImage(t *testing.T) {
	dockerfile := string(Dockerfile)
	packages := baseAptPackages(dockerfile)
	if len(packages) == 0 {
		t.Fatal("the Dockerfile's base apt-get install list was not found")
	}
	if slices.Contains(packages, installProofPackage) {
		t.Errorf("the image's apt list carries the install-proof package %q", installProofPackage)
	}
	if !strings.Contains(fullControlProbe, "--no-install-recommends "+installProofPackage+";") {
		t.Errorf("fullControlProbe does not install %q", installProofPackage)
	}
	for _, step := range []string{
		"apt-get install -y -q --no-install-recommends " + installProofPackage + "; \\",
		"apt-get purge -y -q --autoremove " + installProofPackage + "; \\",
	} {
		if !strings.Contains(dockerfile, step) {
			t.Errorf("the Dockerfile's sudo proof does not run %q", step)
		}
	}
}

// TestBaseAptPackages_ParsesAndRefuses proves the parser reads a well-formed
// list and returns nil for a Dockerfile without one, so the guards above
// cannot pass on an empty list.
func TestBaseAptPackages_ParsesAndRefuses(t *testing.T) {
	good := baseStageFrom + "\nRUN set -eux; \\\n    apt-get install -y --no-install-recommends \\\n        git \\\n        jq; \\\n    rm -rf /var/lib/apt/lists/*\n"
	if got := baseAptPackages(good); !slices.Equal(got, []string{"git", "jq"}) {
		t.Errorf("baseAptPackages(well-formed) = %v, want [git jq]", got)
	}
	for name, dockerfile := range map[string]string{
		"no final stage":    "FROM golang:trixie\nRUN apt-get install -y --no-install-recommends \\\n    jq; \\\n",
		"no install":        baseStageFrom + "\nRUN true\n",
		"unterminated list": baseStageFrom + "\nRUN apt-get install -y --no-install-recommends \\\n    jq \\\n",
	} {
		if got := baseAptPackages(dockerfile); got != nil {
			t.Errorf("%s: baseAptPackages = %v, want nil", name, got)
		}
	}
}

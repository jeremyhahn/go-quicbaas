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
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

// variantFixedPackageFloors is every package the variant vulnerability scan
// flagged with a Debian security fix, at its fixed version. Each variant
// Dockerfile's floor list must carry all of them.
var variantFixedPackageFloors = []string{
	"libkrb5-3=1.21.3-5+deb13u2",
	"libgssapi-krb5-2=1.21.3-5+deb13u2",
	"libk5crypto3=1.21.3-5+deb13u2",
	"libkrb5support0=1.21.3-5+deb13u2",
	"libgssrpc4t64=1.21.3-5+deb13u2",
	"libkadm5clnt-mit12=1.21.3-5+deb13u2",
	"libkadm5srv-mit12=1.21.3-5+deb13u2",
	"libkdb5-10t64=1.21.3-5+deb13u2",
	"libkrb5-dev=1.21.3-5+deb13u2",
	"krb5-multidev=1.21.3-5+deb13u2",
	"linux-libc-dev=6.12.118-1",
	"libwebkit2gtk-4.1-0=2.54.3-1~deb13u1",
	"libjavascriptcoregtk-4.1-0=2.54.3-1~deb13u1",
	"libwebkit2gtk-4.1-dev=2.54.3-1~deb13u1",
	"libjavascriptcoregtk-4.1-dev=2.54.3-1~deb13u1",
	"gir1.2-webkit2-4.1=2.54.3-1~deb13u1",
	"gir1.2-javascriptcoregtk-4.1=2.54.3-1~deb13u1",
}

func readVariantDockerfile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("variants", name))
	if err != nil {
		t.Fatalf("read variant %s: %v", name, err)
	}
	return string(data)
}

// TestVariantDockerfiles_UpgradeAndCheckFixedPackages proves each variant
// upgrades the base image's packages in its own apt RUN, and fails its build
// when a package the scan flagged is still below its fixed version.
func TestVariantDockerfiles_UpgradeAndCheckFixedPackages(t *testing.T) {
	for _, name := range []string{"go-xkms.Dockerfile", "trusted-firmware.Dockerfile"} {
		t.Run(name, func(t *testing.T) {
			dockerfile := readVariantDockerfile(t, name)
			updateAt := strings.Index(dockerfile, " update \\")
			upgradeAt := strings.Index(dockerfile, " upgrade -y \\")
			installAt := strings.Index(dockerfile, " install -y ")
			if updateAt == -1 || upgradeAt == -1 || installAt == -1 || !(updateAt < upgradeAt && upgradeAt < installAt) {
				t.Errorf("want apt-get update, then upgrade -y, then install -y; got offsets %d, %d, %d", updateAt, upgradeAt, installAt)
			}
			for _, floor := range variantFixedPackageFloors {
				if !strings.Contains(dockerfile, floor) {
					t.Errorf("the floor list does not carry %s", floor)
				}
			}
			checkAt := strings.Index(dockerfile, `dpkg --compare-versions "$v" ge "$min"`)
			cleanAt := strings.Index(dockerfile, "rm -rf /var/lib/apt/lists/*")
			if checkAt == -1 || cleanAt == -1 || checkAt > cleanAt {
				t.Errorf("want the dpkg --compare-versions floor check before the apt lists are removed; got offsets %d, %d", checkAt, cleanAt)
			}
		})
	}
}

// variantFloorCheckScript cuts the `for floor in ... done` loop out of a
// variant Dockerfile and joins its backslash-newline continuations, as Docker
// does before it hands RUN text to /bin/sh -c.
func variantFloorCheckScript(t *testing.T, dockerfile string) string {
	t.Helper()
	start := strings.Index(dockerfile, "for floor in")
	if start == -1 {
		t.Fatalf("the Dockerfile has no `for floor in` loop")
	}
	end := strings.Index(dockerfile[start:], "done")
	if end == -1 {
		t.Fatalf("the `for floor in` loop has no `done`")
	}
	script := strings.ReplaceAll(dockerfile[start:start+end+len("done")], "\\\n", " ")
	if !strings.Contains(script, "exit 1") {
		t.Fatalf("the floor check loop does not exit 1 on a below-floor package:\n%s", script)
	}
	return script
}

// runVariantFloorCheck runs a floor check script with a stub dpkg-query that
// reports installed's versions (and knows no other package); the real dpkg
// compares the versions.
func runVariantFloorCheck(t *testing.T, script string, installed map[string]string) (string, error) {
	t.Helper()
	bin := t.TempDir()
	pkgs := t.TempDir()
	for pkg, v := range installed {
		if err := os.WriteFile(filepath.Join(pkgs, pkg), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stub := "#!/bin/sh\nfor last; do :; done\n[ -f \"$FAKE_PKG_DIR/$last\" ] || exit 1\ncat \"$FAKE_PKG_DIR/$last\"\n"
	if err := os.WriteFile(filepath.Join(bin, "dpkg-query"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_PKG_DIR="+pkgs)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// TestVariantDockerfiles_FloorCheckRejectsBelowFloor runs each variant's
// floor check against controlled package versions, so a build with a package
// below its fixed version fails and one at or above every floor passes.
func TestVariantDockerfiles_FloorCheckRejectsBelowFloor(t *testing.T) {
	if _, err := exec.LookPath("dpkg"); err != nil {
		t.Fatalf("dpkg is required to run the variant floor check: %v", err)
	}
	type floor struct{ pkg, min string }
	var floors []floor
	for _, entry := range variantFixedPackageFloors {
		pkg, min, _ := strings.Cut(entry, "=")
		floors = append(floors, floor{pkg, min})
	}
	at := func(suffix func(f floor) string) map[string]string {
		installed := map[string]string{}
		for _, f := range floors {
			installed[f.pkg] = f.min + suffix(f)
		}
		return installed
	}
	for _, name := range []string{"go-xkms.Dockerfile", "trusted-firmware.Dockerfile"} {
		t.Run(name, func(t *testing.T) {
			script := variantFloorCheckScript(t, readVariantDockerfile(t, name))
			for _, pass := range []struct {
				name      string
				installed map[string]string
			}{
				{"equal", at(func(floor) string { return "" })},
				{"newer", at(func(floor) string { return "+1" })},
				{"not installed", map[string]string{}},
			} {
				t.Run(pass.name, func(t *testing.T) {
					if stderr, err := runVariantFloorCheck(t, script, pass.installed); err != nil {
						t.Errorf("floor check failed: %v\n%s", err, stderr)
					}
				})
			}
			for _, below := range floors {
				t.Run("below/"+below.pkg, func(t *testing.T) {
					installed := at(func(f floor) string {
						if f.pkg == below.pkg {
							return "~"
						}
						return ""
					})
					stderr, err := runVariantFloorCheck(t, script, installed)
					if err == nil {
						t.Fatalf("floor check passed with %s at %s~", below.pkg, below.min)
					}
					want := below.pkg + " " + below.min + "~ is below its fixed version " + below.min
					if !strings.Contains(stderr, want) {
						t.Errorf("stderr = %q, want it to contain %q", stderr, want)
					}
				})
			}
		})
	}
}

// variantTrivyArgPattern captures the trusted-firmware variant's trivy pin.
var variantTrivyArgPattern = regexp.MustCompile(`(?m)^ARG (TRIVY_VERSION|TRIVY_SHA256_AMD64|TRIVY_SHA256_ARM64)=(\S+)$`)

// TestTrustedFirmwareVariant_TrivyPinIsTheToolchainPin proves the variant's
// trivy is the repository's one trivy pin (pkg/version/toolchain.mk): the
// same release and the same two release SHA-256s.
func TestTrustedFirmwareVariant_TrivyPinIsTheToolchainPin(t *testing.T) {
	dockerfile := readVariantDockerfile(t, "trusted-firmware.Dockerfile")
	args := map[string]string{}
	for _, match := range variantTrivyArgPattern.FindAllStringSubmatch(dockerfile, -1) {
		args[match[1]] = match[2]
	}
	toolchain, err := version.Toolchain()
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{
		{"TRIVY_VERSION", "TRIVY_VERSION"},
		{"TRIVY_SHA256_AMD64", "TRIVY_SHA256_X86_64"},
		{"TRIVY_SHA256_ARM64", "TRIVY_SHA256_AARCH64"},
	} {
		variant, pin := args[pair[0]], toolchain[pair[1]]
		if variant == "" || variant != pin {
			t.Errorf("trusted-firmware.Dockerfile ARG %s = %q, toolchain.mk %s = %q", pair[0], variant, pair[1], pin)
		}
	}
}

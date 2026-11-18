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
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

// The worker-image workflow's layer scan runs `trivy fs --scanners secret`
// over every extracted layer tarball and hands the JSON report to
// check-secret-findings.sh, which allows a finding only when its image path
// is in secret-allowlist.txt AND the file's sha256 is the pinned one. These
// tests drive the script over temp-dir fixtures shaped like that scan root
// (<root>/<layer-dir>/<image path>) and trivy's JSON report.
const (
	secretFindingsScript = "check-secret-findings.sh"
	secretAllowlistFile  = "secret-allowlist.txt"
	// The one allowlisted image path and its sha256 at the pinned Go release.
	allowedGoTestKeyPath   = "usr/local/go/src/crypto/x509/platform_root_key.pem"
	allowedGoTestKeySHA256 = "2fea2ebe77388df61566fcd43eb0070dbff2842c8786a0e7906a13e5f10c3e65"
	// The checker never reads a finding's content, only its path and hash.
	otherFindingContent = "a fixture standing in for a secret\n"
)

// trivySecretTarget is one Results[] entry of a `trivy fs --format json`
// report carrying secret findings (field names as trivy v0.76.0 writes them).
type trivySecretTarget struct {
	Target  string
	Class   string
	Secrets []map[string]any
}

// writeTrivyReport writes a trivy JSON report naming one HIGH private-key
// finding per target, and returns its path.
func writeTrivyReport(t *testing.T, targets ...string) string {
	t.Helper()
	results := make([]trivySecretTarget, 0, len(targets))
	for _, target := range targets {
		results = append(results, trivySecretTarget{
			Target: target,
			Class:  "secret",
			Secrets: []map[string]any{{
				"RuleID":    "private-key",
				"Category":  "AsymmetricPrivateKey",
				"Severity":  "HIGH",
				"Title":     "Asymmetric Private Key",
				"StartLine": 1,
				"EndLine":   1,
				"Match":     "*****",
			}},
		})
	}
	report := map[string]any{"SchemaVersion": 2, "ArtifactType": "filesystem"}
	if len(results) > 0 {
		report["Results"] = results
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// placeFile writes content at <root>/<rel>, creating its directories.
func placeFile(t *testing.T, root, rel string, content []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// goTestKey returns the real platform_root_key.pem of the Go toolchain the
// test runs under, skipping only when that toolchain does not ship it.
func goTestKey(t *testing.T) []byte {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "src", "crypto", "x509", "platform_root_key.pem"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("this Go toolchain ships no src/crypto/x509/platform_root_key.pem")
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// runSecretFindings runs the checker and returns its exit code and output.
func runSecretFindings(t *testing.T, report, root string) (int, string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "bash", secretFindingsScript, report, root).CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("running %s: %v\n%s", secretFindingsScript, err, out)
	}
	return exitErr.ExitCode(), string(out)
}

func TestSecretFindings_NoFindingsPass(t *testing.T) {
	root := t.TempDir()
	if code, out := runSecretFindings(t, writeTrivyReport(t), root); code != 0 {
		t.Fatalf("a report with no findings exited %d, want 0:\n%s", code, out)
	}
}

func TestSecretFindings_ThePinnedGoTestKeyPasses(t *testing.T) {
	key := goTestKey(t)
	if got := sha256Hex(key); got != allowedGoTestKeySHA256 {
		t.Fatalf("this toolchain's platform_root_key.pem hashes to %s, not the pinned %s", got, allowedGoTestKeySHA256)
	}
	root := t.TempDir()
	placeFile(t, root, "19/"+allowedGoTestKeyPath, key)
	if code, out := runSecretFindings(t, writeTrivyReport(t, "19/"+allowedGoTestKeyPath), root); code != 0 {
		t.Fatalf("the pinned Go test key exited %d, want 0:\n%s", code, out)
	}
	// A target named by its full path under the scan root is the same file.
	if code, out := runSecretFindings(t, writeTrivyReport(t, filepath.Join(root, "19", allowedGoTestKeyPath)), root); code != 0 {
		t.Fatalf("the pinned Go test key named by its absolute path exited %d, want 0:\n%s", code, out)
	}
}

func TestSecretFindings_TheAllowlistedPathWithOtherContentFails(t *testing.T) {
	changed := append(goTestKey(t), '\n')
	root := t.TempDir()
	placeFile(t, root, "19/"+allowedGoTestKeyPath, changed)
	code, out := runSecretFindings(t, writeTrivyReport(t, "19/"+allowedGoTestKeyPath), root)
	if code == 0 {
		t.Fatalf("the allowlisted path with changed content exited 0:\n%s", out)
	}
	for _, want := range []string{allowedGoTestKeyPath, allowedGoTestKeySHA256, sha256Hex(changed)} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not name %q:\n%s", want, out)
		}
	}
}

func TestSecretFindings_AnyOtherFindingFails(t *testing.T) {
	root := t.TempDir()
	placeFile(t, root, "3/etc/key.pem", []byte(otherFindingContent))
	code, out := runSecretFindings(t, writeTrivyReport(t, "3/etc/key.pem"), root)
	if code == 0 {
		t.Fatalf("an unlisted finding exited 0:\n%s", out)
	}
	if !strings.Contains(out, "etc/key.pem") {
		t.Errorf("output does not name etc/key.pem:\n%s", out)
	}
}

func TestSecretFindings_TheAllowlistedKeyDoesNotCoverAnotherFinding(t *testing.T) {
	key := goTestKey(t)
	root := t.TempDir()
	placeFile(t, root, "19/"+allowedGoTestKeyPath, key)
	placeFile(t, root, "3/etc/key.pem", []byte(otherFindingContent))
	code, out := runSecretFindings(t, writeTrivyReport(t, "19/"+allowedGoTestKeyPath, "3/etc/key.pem"), root)
	if code == 0 {
		t.Fatalf("the allowlisted key plus another finding exited 0:\n%s", out)
	}
	if !strings.Contains(out, "etc/key.pem") {
		t.Errorf("output does not name etc/key.pem:\n%s", out)
	}
}

func TestSecretFindings_AnUnreadableReportFails(t *testing.T) {
	root := t.TempDir()
	dir := t.TempDir()
	notJSON := filepath.Join(dir, "not-json.json")
	if err := os.WriteFile(notJSON, []byte("trivy crashed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	notAReport := filepath.Join(dir, "array.json")
	if err := os.WriteFile(notAReport, []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, report := range map[string]string{
		"missing":      filepath.Join(dir, "absent.json"),
		"not JSON":     notJSON,
		"empty":        empty,
		"not a report": notAReport,
	} {
		if code, out := runSecretFindings(t, report, root); code == 0 {
			t.Errorf("a %s findings file exited 0:\n%s", name, out)
		}
	}
}

// pinnedGoVersion returns the Go version secret-allowlist.txt records, and
// fails unless every entry records the same one.
func pinnedGoVersion(t *testing.T) string {
	t.Helper()
	f, err := os.Open(secretAllowlistFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	pinned := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || !strings.HasPrefix(fields[0], "go") {
			t.Fatalf("%s: entry %q is not `go<version> <path> <sha256>`", secretAllowlistFile, line)
		}
		v := strings.TrimPrefix(fields[0], "go")
		if pinned != "" && v != pinned {
			t.Fatalf("%s records two Go versions, %s and %s", secretAllowlistFile, pinned, v)
		}
		pinned = v
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if pinned == "" {
		t.Fatalf("%s has no entry", secretAllowlistFile)
	}
	return pinned
}

// A Go bump changes the pinned test key's hash; the bump must refresh the
// allowlist in the same change, or this fails.
func TestSecretAllowlist_IsPinnedToTheToolchainGoVersion(t *testing.T) {
	toolchain, err := version.Toolchain()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pinnedGoVersion(t), toolchain["GO_VERSION"]; got != want {
		t.Fatalf("%s is pinned to go%s, toolchain.mk GO_VERSION is %s: refresh the hashes for go%s", secretAllowlistFile, got, want, want)
	}
}

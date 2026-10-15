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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// writeQRDB writes a stand-in for the go-qrdb sdk/go module root, carrying the
// two files the canonical names are declared in.
func writeQRDB(t *testing.T, barrier, ceremony string) string {
	t.Helper()
	dir := t.TempDir()
	transport := filepath.Join(dir, "transport")
	if err := os.MkdirAll(transport, 0o755); err != nil {
		t.Fatalf("creating the fixture transport directory: %v", err)
	}
	for name, content := range map[string]string{
		"barrier_types.go":  "package transport\n\n" + barrier,
		"ceremony_types.go": "package transport\n\n" + ceremony,
	} {
		if err := os.WriteFile(filepath.Join(transport, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

// writeTS writes a stand-in TypeScript types file.
func writeTS(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "types.ts")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing the fixture types.ts: %v", err)
	}
	return path
}

func TestRunPassesWhenEveryGoTypeIsMirrored(t *testing.T) {
	qrdb := writeQRDB(t,
		"type BarrierStatusResponse struct{}\n"+
			"type BarrierUnsealRequest struct{}\n"+
			// The verbose upstream spelling normalizes onto the TS SDK's.
			"type BarrierInitializeShamirRequest struct{}\n"+
			"type BootstrapBundleRequest struct{}\n",
		// go-qrdb declares this without the Ceremony prefix the TS SDK gives it.
		"type EnrollExternalOfficerRequest struct{}\n"+
			"type CeremonyStatusResponse struct{}\n")
	ts := writeTS(t,
		"export interface BarrierStatusResponse {}\n"+
			"export interface BarrierUnsealRequest {}\n"+
			"export interface BarrierShamirInitRequest {}\n"+
			"export type BarrierShamirRequest = unknown;\n"+
			"export interface BootstrapBundleRequest {}\n"+
			"export interface CeremonyEnrollExternalOfficerRequest {}\n"+
			"export interface CeremonyStatusResponse {}\n"+
			// A local convenience alias: the TS surface may be a strict superset.
			"export interface BarrierLocalConvenience {}\n")

	var stdout, stderr strings.Builder
	status := run([]string{"-qrdb-dir", qrdb, "-ts-types", ts}, &stdout, &stderr)
	if status != gate.ExitClean {
		t.Fatalf("run exited %d, want %d\nstdout: %s\nstderr: %s",
			status, gate.ExitClean, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "every Go barrier/ceremony/bootstrap type is mirrored") {
		t.Fatalf("the pass was not reported: %q", stdout.String())
	}
}

func TestRunFailsOnAGoTypeWithNoTypeScriptCounterpart(t *testing.T) {
	// THE CASE THAT MATTERS. This is the drift class the gate exists for: a type
	// added or renamed upstream that the downstream SDK does not know about.
	qrdb := writeQRDB(t,
		"type BarrierStatusResponse struct{}\n"+
			"type BarrierBrandNewVerbRequest struct{}\n",
		"type CeremonyStatusResponse struct{}\n")
	ts := writeTS(t,
		"export interface BarrierStatusResponse {}\n"+
			"export interface CeremonyStatusResponse {}\n")

	var stdout, stderr strings.Builder
	status := run([]string{"-qrdb-dir", qrdb, "-ts-types", ts}, &stdout, &stderr)
	if status != gate.ExitFindings {
		t.Fatalf("run exited %d, want %d\nstdout: %s", status, gate.ExitFindings, stdout.String())
	}
	report := stdout.String()
	if !strings.Contains(report, "Go types missing from TypeScript SDK") {
		t.Fatalf("the failure was not reported: %s", report)
	}
	if !strings.Contains(report, "BarrierBrandNewVerbRequest") {
		t.Fatalf("the report does not name the drifted type: %s", report)
	}
	if strings.Contains(report, "BarrierStatusResponse") {
		t.Fatalf("the report names a type that IS mirrored: %s", report)
	}
}

func TestRunHonoursTheTrackedExemptions(t *testing.T) {
	// knownUnmirrored is the tracking mechanism for drift that a product
	// decision owns (go-quicbaas#946), not a way to make the gate unable to
	// fail: the entry must be exact, a neighbouring name still fails, and a
	// name whose drift was closed fails again when the TS mirror goes missing.
	qrdb := writeQRDB(t,
		"type BarrierStatusResponse struct{}\n"+
			"type BarrierSealRequest struct{}\n"+
			"type BarrierEnableEncryptionRequest struct{}\n"+
			"type BarrierEnableEncryptionResponse struct{}\n",
		"type CeremonyStatusResponse struct{}\n")
	ts := writeTS(t,
		"export interface BarrierStatusResponse {}\n"+
			"export interface CeremonyStatusResponse {}\n")

	var stdout, stderr strings.Builder
	status := run([]string{"-qrdb-dir", qrdb, "-ts-types", ts}, &stdout, &stderr)
	if status != gate.ExitFindings {
		t.Fatalf("run exited %d, want %d\nstdout: %s", status, gate.ExitFindings, stdout.String())
	}
	report := stdout.String()
	for _, closed := range []string{"BarrierEnableEncryptionRequest", "BarrierEnableEncryptionResponse"} {
		if !strings.Contains(report, closed) {
			t.Fatalf("%s is mirrored in TS and no longer exempt, so it must be reported: %s", closed, report)
		}
	}
	if strings.Contains(report, "BarrierSealRequest\n") {
		t.Fatalf("BarrierSealRequest is on the exemption list and must not be reported: %s", report)
	}
}

func TestRunRefusesToCompareNothing(t *testing.T) {
	// Every one of these used to be a silent `exit 0`. An input the gate cannot
	// read is a tool error naming what is missing — never a pass.
	good := "type BarrierStatusResponse struct{}\n"
	cases := []struct {
		name string
		args func(t *testing.T) []string
	}{
		{
			name: "the canonical Go files are absent",
			args: func(t *testing.T) []string {
				return []string{"-qrdb-dir", t.TempDir(), "-ts-types", writeTS(t, "export interface BarrierStatusResponse {}\n")}
			},
		},
		{
			name: "the TypeScript file is absent",
			args: func(t *testing.T) []string {
				return []string{"-qrdb-dir", writeQRDB(t, good, ""), "-ts-types", filepath.Join(t.TempDir(), "nonesuch.ts")}
			},
		},
		{
			name: "the Go extraction matched nothing",
			args: func(t *testing.T) []string {
				return []string{"-qrdb-dir", writeQRDB(t, "type Unrelated struct{}\n", ""),
					"-ts-types", writeTS(t, "export interface BarrierStatusResponse {}\n")}
			},
		},
		{
			name: "the TypeScript extraction matched nothing",
			args: func(t *testing.T) []string {
				return []string{"-qrdb-dir", writeQRDB(t, good, ""), "-ts-types", writeTS(t, "export interface Unrelated {}\n")}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			if status := run(tc.args(t), &stdout, &stderr); status != gate.ExitToolError {
				t.Fatalf("run exited %d, want %d (a skip is not an answer)\nstdout: %s\nstderr: %s",
					status, gate.ExitToolError, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "FAIL") {
				t.Fatalf("the failure was not reported: %q", stderr.String())
			}
		})
	}
}

func TestRunRejectsAnUnknownFlag(t *testing.T) {
	var stdout, stderr strings.Builder
	if status := run([]string{"-nonesuch"}, &stdout, &stderr); status != gate.ExitToolError {
		t.Fatalf("run exited %d, want %d", status, gate.ExitToolError)
	}
}

func TestNormalizeCollapsesConventionAndNotMeaning(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "BarrierInitializeShamirRequest", want: "BarrierShamirRequest"},
		{in: "BarrierInitializeShamirResponse", want: "BarrierShamirResponse"},
		{in: "CeremonyOfficerRequest", want: "CeremonyOfficer"},
		// Untouched: these are names, not conventions.
		{in: "BarrierStatusResponse", want: "BarrierStatusResponse"},
		{in: "BarrierSealRequest", want: "BarrierSealRequest"},
		{in: "BootstrapBundleResponse", want: "BootstrapBundleResponse"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalize(tc.in); got != tc.want {
				t.Fatalf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRewriteGoPrefixGivesTheDownstreamPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "EnrollExternalOfficerRequest", want: "CeremonyEnrollExternalOfficerRequest"},
		{in: "EnrollExternalOfficerResponse", want: "CeremonyEnrollExternalOfficerResponse"},
		{in: "BarrierStatusResponse", want: "BarrierStatusResponse"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := rewriteGoPrefix(tc.in); got != tc.want {
				t.Fatalf("rewriteGoPrefix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEveryExemptionCarriesAReason(t *testing.T) {
	// Each entry is a name nobody is checking. An empty reason is how a list
	// like this stops being reviewable.
	for name, reason := range knownUnmirrored {
		if strings.TrimSpace(reason) == "" {
			t.Fatalf("%s is exempted with no reason", name)
		}
	}
}

func TestResolveModuleDirReportsWhatTheGoToolSaid(t *testing.T) {
	// The lab path this replaces produced a directory that did not exist and a
	// status of 0; the failure now carries the go tool's own complaint.
	_, err := resolveModuleDir(".", "example.invalid/not/a/dependency")
	var resolve *ModuleResolveError
	if !errors.As(err, &resolve) {
		t.Fatalf("resolveModuleDir = %v, want *ModuleResolveError", err)
	}
	if resolve.Unwrap() == nil {
		t.Fatal("ModuleResolveError must carry the underlying failure")
	}
	if !strings.Contains(resolve.Error(), "example.invalid/not/a/dependency") {
		t.Fatalf("the error does not name the module: %s", resolve.Error())
	}
	if !strings.Contains(resolve.Error(), "is it in go.mod") {
		t.Fatalf("the error does not say what to check: %s", resolve.Error())
	}
	if strings.TrimSpace(resolve.Output) == "" {
		t.Fatalf("the error does not carry the go tool's complaint: %+v", resolve)
	}
}

func TestTheDefaultInputsResolveToRealFiles(t *testing.T) {
	// This is what replaces the lab mount point, and it is the half of the gate
	// that used to be silently unsatisfiable. Whether the TypeScript surface
	// currently mirrors go-qrdb is `make verify-api-shapes`'s question, not this
	// suite's — asserting it here would make `make test` fail for a product
	// drift. What is asserted is that both inputs are REACHABLE by default.
	const repoRoot = "../.."

	dir, err := resolveModuleDir(repoRoot, qrdbSDKModule)
	if err != nil {
		t.Fatalf("resolving %s from the module graph: %v", qrdbSDKModule, err)
	}
	names, err := goTypes(dir)
	if err != nil {
		t.Fatalf("reading the canonical Go types from %s: %v", dir, err)
	}
	if len(names) == 0 {
		t.Fatal("the canonical Go type set is empty")
	}

	tsNames, err := tsTypesFrom(filepath.Join(repoRoot, defaultTSTypes))
	if err != nil {
		t.Fatalf("reading the default TypeScript surface: %v", err)
	}
	if len(tsNames) == 0 {
		t.Fatal("the TypeScript type set is empty")
	}
}

func TestEmptyTypeSetErrorNamesTheSideAndTheSource(t *testing.T) {
	err := &EmptyTypeSetError{Side: "Go", Source: "/mod"}
	for _, want := range []string{"Go", "/mod", "stopped matching"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error omits %q: %s", want, err.Error())
		}
	}
}

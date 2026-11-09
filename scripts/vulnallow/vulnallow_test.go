package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	osvMCP = `{"osv": {"id": "GO-2026-6911", "summary": "SSRF and credential leak via unvalidated Location header in github.com/mark3labs/mcp-go"}}`

	findingMCPReachable = `{"finding": {"osv": "GO-2026-6911", "trace": [
		{"module": "github.com/mark3labs/mcp-go", "version": "v0.45.0", "package": "github.com/mark3labs/mcp-go/client/transport", "function": "NewStreamableHTTP", "position": {"filename": "client/transport/streamable_http.go", "line": 120}},
		{"module": "github.com/jeremyhahn/go-quicbaas", "package": "github.com/jeremyhahn/go-quicbaas/pkg/server", "function": "Start", "receiver": "*Server", "position": {"filename": "pkg/server/server.go", "line": 1689}}]}}`
	findingMCPFixed = `{"finding": {"osv": "GO-2026-6911", "fixed_version": "v0.46.0", "trace": [
		{"module": "github.com/mark3labs/mcp-go", "version": "v0.45.0", "package": "github.com/mark3labs/mcp-go/client/transport", "function": "NewStreamableHTTP", "position": {"filename": "client/transport/streamable_http.go", "line": 120}},
		{"module": "github.com/jeremyhahn/go-quicbaas", "package": "github.com/jeremyhahn/go-quicbaas/pkg/server", "function": "Start", "receiver": "*Server", "position": {"filename": "pkg/server/server.go", "line": 1689}}]}}`
	findingMCPOtherModuleFixed = `{"finding": {"osv": "GO-2026-6911", "fixed_version": "v1.2.3", "trace": [
		{"module": "github.com/example/othermcp", "version": "v1.2.0", "package": "github.com/example/othermcp/transport", "function": "Dial", "position": {"filename": "transport/dial.go", "line": 42}},
		{"module": "github.com/jeremyhahn/go-quicbaas", "package": "github.com/jeremyhahn/go-quicbaas/pkg/server", "function": "Start", "receiver": "*Server", "position": {"filename": "pkg/server/server.go", "line": 1689}}]}}`

	osvOther = `{"osv": {"id": "GO-2026-0001", "summary": "Denial of service in example.com/lib"}}`

	findingOtherReachable = `{"finding": {"osv": "GO-2026-0001", "fixed_version": "v1.2.3", "trace": [
		{"module": "example.com/lib", "version": "v1.2.0", "package": "example.com/lib", "function": "Parse", "position": {"filename": "lib/parse.go", "line": 42}}]}}`

	findingOtherModuleLevel = `{"finding": {"osv": "GO-2026-0001", "fixed_version": "v1.2.3", "trace": [
		{"module": "example.com/lib", "version": "v1.2.0"}]}}`

	configAndProgress = `{"config": {"protocol_version": "v1.0.0", "scanner_name": "govulncheck"}}
{"progress": {"message": "Scanning your code and 100 packages across 10 dependent modules for known vulnerabilities..."}}`

	allowMCP = "# header comment\n\nGO-2026-6911 mcp-go has no fixed release (go-quicbaas#2211)\n"
)

// runWith writes the allowlist to a temporary file and runs the command on
// the stream, returning the exit code, stdout and stderr.
func runWith(t *testing.T, allowlist, stream string) (int, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vuln-allowlist.txt")
	if err := os.WriteFile(path, []byte(allowlist), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"vulnallow", path}, strings.NewReader(stream), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunListedReachableAdvisoryIsAllowed(t *testing.T) {
	stream := configAndProgress + "\n" + osvMCP + "\n" + findingMCPReachable + "\n" + findingMCPReachable
	code, stdout, stderr := runWith(t, allowMCP, stream)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	want := "vulnallow: GO-2026-6911 is allowed: mcp-go has no fixed release (go-quicbaas#2211)\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestRunListedAdvisoryWithAFixedReleaseFails(t *testing.T) {
	code, stdout, stderr := runWith(t, allowMCP, osvMCP+"\n"+findingMCPFixed)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, want := range []string{"GO-2026-6911", "has a fixed release (v0.46.0)", "remove the entry"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want it to contain %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "is allowed") {
		t.Errorf("stdout = %q, want no allowed line", stdout)
	}
}

func TestRunListedAdvisoryFixableInAnyFindingFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream string
	}{
		{"no-fix finding first", osvMCP + "\n" + findingMCPReachable + "\n" + findingMCPOtherModuleFixed},
		{"fixed finding first", osvMCP + "\n" + findingMCPOtherModuleFixed + "\n" + findingMCPReachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runWith(t, allowMCP, tc.stream)
			if code != 1 {
				t.Fatalf("exit = %d, want 1; stdout=%q stderr=%q", code, stdout, stderr)
			}
			for _, want := range []string{"GO-2026-6911", "has a fixed release (v1.2.3)"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout = %q, want it to contain %q", stdout, want)
				}
			}
			if strings.Contains(stdout, "is allowed") {
				t.Errorf("stdout = %q, want no allowed line", stdout)
			}
		})
	}
}

func TestRunUnlistedReachableAdvisoryFails(t *testing.T) {
	stream := osvMCP + osvOther + findingMCPReachable + findingOtherReachable
	code, stdout, _ := runWith(t, allowMCP, stream)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%q", code, stdout)
	}
	for _, want := range []string{
		"vulnallow: GO-2026-0001 is reachable and not on ",
		"summary:  Denial of service in example.com/lib",
		"found in: example.com/lib@v1.2.0",
		"fixed in: v1.2.3",
		"trace:    lib/parse.go:42",
		"vulnallow: GO-2026-6911 is allowed: ",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

func TestRunUnlistedFindingWithNoFixShowsNA(t *testing.T) {
	code, stdout, _ := runWith(t, "", osvMCP+findingMCPReachable)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	for _, want := range []string{
		"found in: github.com/mark3labs/mcp-go@v0.45.0",
		"fixed in: N/A",
		"trace:    client/transport/streamable_http.go:120",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

func TestRunModuleLevelFindingIsNotReachable(t *testing.T) {
	code, stdout, stderr := runWith(t, "", osvOther+findingOtherModuleLevel)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestRunStaleEntryFails(t *testing.T) {
	code, stdout, _ := runWith(t, allowMCP, osvOther+findingOtherModuleLevel)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stdout, "vulnallow: GO-2026-6911 on ") ||
		!strings.Contains(stdout, "vuln-allowlist.txt:3 matches no reachable finding; remove it") {
		t.Errorf("stdout lacks the stale line:\n%s", stdout)
	}
}

func TestParseAllowlistRejectsBadEntries(t *testing.T) {
	tests := []struct {
		name     string
		list     string
		wantLine int
		check    func(error) bool
	}{
		{
			name:     "no reason",
			list:     "# c\nGO-2026-6911\n",
			wantLine: 2,
			check:    func(err error) bool { var e *MissingReasonError; return errors.As(err, &e) && e.Line == 2 },
		},
		{
			name:     "duplicate ID",
			list:     "GO-2026-6911 a reason\n\nGO-2026-6911 another reason\n",
			wantLine: 3,
			check: func(err error) bool {
				var e *DuplicateIDError
				return errors.As(err, &e) && e.Line == 3 && e.FirstLine == 1
			},
		},
		{
			name:     "non-GO ID",
			list:     "GO-2026-6911 a reason\nCVE-2026-1234 a reason\n",
			wantLine: 2,
			check:    func(err error) bool { var e *BadIDError; return errors.As(err, &e) && e.Line == 2 },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAllowlist("list.txt", strings.NewReader(tt.list))
			if err == nil {
				t.Fatal("ParseAllowlist returned no error")
			}
			if !tt.check(err) {
				t.Errorf("error %T %q is not the expected type and line", err, err)
			}
			wantText := "list.txt:" + strconv.Itoa(tt.wantLine) + ":"
			if !strings.Contains(err.Error(), wantText) {
				t.Errorf("error %q does not name %q", err, wantText)
			}
		})
	}
}

func TestRunBadAllowlistFails(t *testing.T) {
	code, _, stderr := runWith(t, "GO-2026-6911\n", osvMCP+findingMCPReachable)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "GO-2026-6911 has no reason") {
		t.Errorf("stderr = %q, want the missing-reason error", stderr)
	}
}

func TestRunMalformedStreamFails(t *testing.T) {
	code, _, stderr := runWith(t, allowMCP, osvMCP+`{"finding": {"osv": `)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "vulnallow: decode govulncheck JSON:") {
		t.Errorf("stderr = %q, want the decode error", stderr)
	}
}

func TestRunMissingAllowlistFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "absent.txt")
	code := run([]string{"vulnallow", missing}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestRunWrongArgumentCountIsUsageError(t *testing.T) {
	for _, args := range [][]string{{"vulnallow"}, {"vulnallow", "a", "b"}} {
		var stdout, stderr bytes.Buffer
		code := run(args, strings.NewReader(""), &stdout, &stderr)
		if code != 2 {
			t.Errorf("run(%q) = %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "usage:") {
			t.Errorf("run(%q) stderr = %q, want usage", args, stderr.String())
		}
	}
}

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package sdlc

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestPublicOutput_RedactsTheShapesABuildStepPrints covers the sanitiser at the
// unit, one shape at a time, so a failure names which shape regressed.
//
// None of these is a real credential; each is the SHAPE of one.
func TestPublicOutput_RedactsTheShapesABuildStepPrints(t *testing.T) {
	for _, tc := range []struct {
		name   string
		raw    string
		secret string
	}{
		{"a github token", "fatal: bad credentials ghp_A1b2C3d4E5f6G7h8I9j0KlMnOpQrStUv", "ghp_A1b2C3d4E5f6G7h8I9j0KlMnOpQrStUv"},
		{"a gitlab token", "remote: HTTP Basic: Access denied glpat-ABCDEFGHIJKLMNOPQRST", "glpat-ABCDEFGHIJKLMNOPQRST"},
		{"an openai-style key", "auth error using sk-abcdefghijklmnopqrstuvwx", "sk-abcdefghijklmnopqrstuvwx"},
		{"an aws access key id", "signature mismatch for AKIAIOSFODNN7EXAMPLE", "AKIAIOSFODNN7EXAMPLE"},
		// Basic, not Bearer: this row must exercise the HEADER pattern, not the
		// bearer one. A row that can pass via a neighbouring pattern does not
		// pin the pattern it names.
		{"an authorization header", "curl sent Authorization: Basic dXNlcjpwYXNzd29yZA==", "dXNlcjpwYXNzd29yZA=="},
		// Proxy-Authorization, not X-API-Key: the assignment pattern also matches
		// `api-key: value`, so an X-API-Key row would not pin the HEADER
		// pattern it claims to.
		{"a proxy-authorization header", "proxy rejected Proxy-Authorization: Basic dXNlcjpwdw==", "dXNlcjpwdw=="},
		{"a set-cookie header", "Set-Cookie: session=abcdef123456; HttpOnly", "abcdef123456"},
		{"a bearer token", "denied; bearer eyJhbGciOiJIUzI1NiJ9xxxxxxxx was rejected", "eyJhbGciOiJIUzI1NiJ9xxxxxxxx"},
		{"an authenticated URL", "fatal: could not read https://ci:s3cr3tvalue@forge.invalid/x.git", "s3cr3tvalue"},
		// No vendor prefix on the value: this row must exercise the ASSIGNMENT
		// pattern rather than the vendor-token one.
		{"an environment assignment", "env: QUICBAAS_FORGE_TOKEN=plainvalue1234567", "plainvalue1234567"},
		{"a go short declaration in a diff", `+  clientSecret := "plain-secret-value"`, "plain-secret-value"},
		{"a quoted json secret", `{"client_secret": "jsonsecretvalue123"}`, "jsonsecretvalue123"},
		{"a quoted value containing spaces", `client_secret: "alpha beta gamma"`, "beta gamma"},
		{"a single-quoted value with spaces", `password: 'one two three'`, "two three"},
		{"a github fine-grained pat", "denied for github_pat_11ABCDEFG0abcdefghijklmnop", "github_pat_11ABCDEFG0abcdefghijklmnop"},
		{"an anthropic project key", "auth failed using sk-ant-api03-abcdefghijklmnopqrst", "sk-ant-api03-abcdefghijklmnopqrst"},
		{"a stripe live key", "charge failed sk_live_abcdefghijklmnop", "sk_live_abcdefghijklmnop"},
		{"a google api key", "quota error for AIzaSyA1b2C3d4E5f6G7h8I9j0KlMnOpQr", "AIzaSyA1b2C3d4E5f6G7h8I9j0KlMnOpQr"},
		{"a slack bot token", "post failed xoxb-1234567890-abcdefghij", "xoxb-1234567890-abcdefghij"},
		{"an aws temporary key id", "expired ASIAIOSFODNN7EXAMPLE", "ASIAIOSFODNN7EXAMPLE"},
		{"a vault token", "permission denied hvs.CAESIJKLMNOPQRSTUVWX", "hvs.CAESIJKLMNOPQRSTUVWX"},
		{"a lowercase secret assignment", "config: api_key: abcd1234efgh5678", "abcd1234efgh5678"},
		{"a JWT", "rejected eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk", "dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
		{"a PEM private key", "leaked:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----", "MIIEowIBAAKCAQEA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The case is only meaningful if the secret is in the input.
			if !strings.Contains(tc.raw, tc.secret) {
				t.Fatalf("the fixture does not contain its own secret; this case proves nothing")
			}
			got := publicOutput(tc.raw)
			if strings.Contains(got, tc.secret) {
				t.Errorf("publicOutput kept the secret.\nin:  %s\nout: %s", tc.raw, got)
			}
			if !strings.Contains(got, redactedMarker) {
				t.Errorf("nothing was marked as redacted, so the reader cannot tell something was removed: %s", got)
			}
		})
	}
}

// TestPublicOutput_BoundsTheDump: the cap matters independently of secrets. An
// unbounded stderr dump in an issue comment is unreadable, and a human who
// cannot read the park cannot act on it.
func TestPublicOutput_BoundsTheDump(t *testing.T) {
	raw := strings.Repeat("noise ", 4000)
	got := publicOutput(raw)
	if len(got) > publicOutputLimit+120 {
		t.Errorf("output is %d bytes, want it bounded near %d", len(got), publicOutputLimit)
	}
	if !strings.Contains(got, "more bytes withheld") {
		t.Errorf("truncation was silent; a reader must know the text was cut: %q", got[max(0, len(got)-80):])
	}
	if !strings.Contains(got, "diagnostic truncated for publication") {
		t.Errorf("truncation note should be source-neutral, got: %q", got[max(0, len(got)-120):])
	}
	if strings.Contains(got, "daemon log") || strings.Contains(got, "worker event") {
		t.Errorf("shared truncation note pointed at a caller-specific log surface: %q", got[max(0, len(got)-120):])
	}
	// Short output is passed through untouched — bounding must not mangle the
	// ordinary case.
	if got := publicOutput("  build failed: undefined: Foo  "); got != "build failed: undefined: Foo" {
		t.Errorf("short output was altered: %q", got)
	}
	if publicOutput("   ") != "" {
		t.Error("blank output should produce nothing to append")
	}
}

// TestPublicOutput_RedactsBeforeBounding pins the ORDER.
//
// Bounding first could cut a credential in half and leave the visible half in
// the comment, which is a leak that looks like a truncation.
func TestPublicOutput_RedactsBeforeBounding(t *testing.T) {
	const secret = "ghp_A1b2C3d4E5f6G7h8I9j0KlMnOpQrStUv"
	// The secret starts well before the limit, so a bound-before-redact
	// implementation would leave a LONG visible prefix — and the assertion
	// checks a short prefix, so it fails on any of it surviving. Placing the
	// secret 10 bytes from the limit and asserting on 20 characters, as an
	// earlier version did, could not fail: only 10 bytes would leak and the
	// 20-character check would miss them.
	// THE SURVIVOR MUST BE TOO SHORT TO RE-MATCH, or the test cannot fail: a
	// bound-before-redact implementation would truncate the token, and if the
	// truncated prefix still satisfies `ghp_[A-Za-z0-9]{16,}` the later
	// redaction pass would clean it up and the test would pass anyway. Ten
	// surviving bytes leave `ghp_` plus six characters, which matches nothing.
	raw := strings.Repeat("x", publicOutputLimit-10) + secret + strings.Repeat("y", 200)
	got := publicOutput(raw)
	if strings.Contains(got, secret[:10]) {
		t.Errorf("a truncated credential survived: %q", got[max(0, len(got)-160):])
	}
}

// TestPublicOutput_DoesNotEatOrdinaryDiagnostics is the other half of the
// bargain.
//
// Over-matching is the safe direction, but it is not free: a sanitiser that
// redacts branch names and stack frames turns a readable failure into a wall of
// [redacted], and a park nobody can read is the silence #198 exists to remove.
// These are shapes a build log really produces and must survive intact.
func TestPublicOutput_DoesNotEatOrdinaryDiagnostics(t *testing.T) {
	for _, keep := range []string{
		"task-test-abcdefghijklmnop failed on branch issue/24-release",
		"disk-abcdefghijklmnopqrs is full",
		"--- FAIL: TestThing (0.03s)\n    thing_test.go:42: want 3, got 4",
		"./pkg/x/y.go:17:9: undefined: Frobnicate",
		"panic: runtime error: index out of range [3] with length 2",
		"X-Request-Id: 7f3c1b2a-9d4e",
	} {
		if got := publicOutput(keep); got != keep {
			t.Errorf("ordinary diagnostic was altered.\nin:  %s\nout: %s", keep, got)
		}
	}

	// A header with an EMPTY value must not swallow the following line.
	const twoLines = "X-API-Key:\nthe build failed in pkg/foo"
	if got := publicOutput(twoLines); !strings.Contains(got, "the build failed in pkg/foo") {
		t.Errorf("an empty header value consumed the next diagnostic line: %q", got)
	}
}

// TestPublicOutput_TruncatesOnARuneBoundary: a byte slice at the limit can split
// a multibyte character, and the JSON encoding then replaces the broken tail
// with U+FFFD — a diagnostic corrupted for no reason (review 1376).
//
// Same family as the partial-redaction mistakes this file has already produced
// four of: an operation that stops in the wrong place and produces something
// that looks fine.
func TestPublicOutput_TruncatesOnARuneBoundary(t *testing.T) {
	// BOTH MULTIBYTE WIDTHS, not just the three-byte one. A back-up sized for
	// three bytes handles "→" and still splits the four-byte "𝄞" — a
	// regression that a three-byte-only sweep reports as green, which is the
	// same looks-fine failure this test exists to catch.
	for _, r := range []string{"→", "𝄞"} {
		width := len(r)
		// Sweeping the offset past one rune's width guarantees at least one
		// case where the naive cut at the limit lands mid-character.
		for pad := 0; pad < width; pad++ {
			name := fmt.Sprintf("%d-byte rune, pad %d", width, pad)
			raw := strings.Repeat("x", pad) + strings.Repeat(r, publicOutputLimit)
			got := publicOutput(raw)
			if !utf8.ValidString(got) {
				t.Errorf("%s: output is not valid UTF-8; a rune was split", name)
			}
			if strings.ContainsRune(got, utf8.RuneError) {
				t.Errorf("%s: output carries U+FFFD, so a character was corrupted", name)
			}
			head := strings.SplitN(got, "\n… (", 2)[0]
			// The cap bounds the kept head, and the back-up may remove at most
			// one rune's width from it — never more.
			if len(head) > publicOutputLimit || len(head) < publicOutputLimit-width {
				t.Errorf("%s: kept head is %d bytes, want within %d of the %d-byte cap",
					name, len(head), width, publicOutputLimit)
			}
			if !strings.Contains(got, "more bytes withheld") {
				t.Errorf("%s: truncation was silent", name)
			}
		}
	}

	// A single-byte-per-rune input is UNAFFECTED, asserted as an exact length
	// rather than as mere validity: every byte below the limit is already a
	// rune start, so a correct back-up removes nothing at all. Checking only
	// that the result parses would pass a version that dropped bytes it had no
	// reason to drop.
	plain := strings.Repeat("a", publicOutputLimit+50)
	got := publicOutput(plain)
	if !utf8.ValidString(got) || !strings.Contains(got, "more bytes withheld") {
		t.Fatalf("ASCII truncation regressed: %q", got[max(0, len(got)-60):])
	}
	if head := strings.SplitN(got, "\n… (", 2)[0]; len(head) != publicOutputLimit {
		t.Errorf("ASCII kept head is %d bytes, want exactly the %d-byte cap; "+
			"the back-up removed bytes from input that had no boundary to find",
			len(head), publicOutputLimit)
	}
}

// TestPublicOutput_InvalidUTF8DoesNotEraseTheDiagnostic bounds the boundary
// search itself.
//
// Subprocess output is ARBITRARY BYTES, not text. An unbounded back-up over a
// long run of continuation bytes walks the cut to zero and withholds the entire
// diagnostic — turning a cosmetic defect (one split character) into the total
// silent loss this file exists to prevent. The back-up is capped at one rune's
// width, past which there is no boundary to find.
func TestPublicOutput_InvalidUTF8DoesNotEraseTheDiagnostic(t *testing.T) {
	// Continuation bytes all the way down: no byte in this input is a rune
	// start, so an unbounded search finds no boundary before offset zero.
	raw := strings.Repeat("\x80", publicOutputLimit+100)
	got := publicOutput(raw)
	if !strings.Contains(got, "more bytes withheld") {
		t.Fatalf("truncation was silent: %q", got)
	}
	head := strings.SplitN(got, "\n… (", 2)[0]
	if len(head) < publicOutputLimit-utf8.UTFMax {
		t.Errorf("the kept head is %d bytes, want it near the %d-byte cap; "+
			"the boundary search walked past its bound and withheld everything",
			len(head), publicOutputLimit)
	}
	if len(got) > publicOutputLimit+120 {
		t.Errorf("output is %d bytes, want it bounded near %d", len(got), publicOutputLimit)
	}
}

// TestPublicVerificationSummary_CarriesNoOutput is #213: whatever a failed
// command printed, including a secret no pattern recognises, none of it is in
// the summary. The summary still names the command, its exit status, the size
// of what was withheld, and the job record that holds the output.
func TestPublicVerificationSummary_CarriesNoOutput(t *testing.T) {
	const bare = "recovery phrase: plover kestrel amber ninety"
	failed := []orchestration.ValidationResult{
		{Name: "make test", ExitCode: 2, Message: bare + "\nmake: *** Error 1", Details: "--- FAIL: TestX"},
		{Name: "go `vet`", Message: "refused"},
	}
	got := publicVerificationSummary("job-7", failed, publicOutputLimit)
	for _, output := range []string{"plover", "make: ***", "FAIL", "refused"} {
		if strings.Contains(got, output) {
			t.Errorf("the summary carries output %q:\n%s", output, got)
		}
	}
	withheld := len(failed[0].Message) + len(failed[0].Details)
	for _, want := range []string{
		"- `make test` exited 2; " + fmt.Sprint(withheld) + " bytes of output withheld from publication",
		"- `go 'vet'` failed; 7 bytes",
		"`" + auditVerification + "` audit record of job job-7.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary lacks %q:\n%s", want, got)
		}
	}
}

// TestPublicVerificationSummary_KeepsThePointerWithinItsBudget: when the
// checks do not all fit, the ones left out are counted and the line saying
// where the output is stays.
func TestPublicVerificationSummary_KeepsThePointerWithinItsBudget(t *testing.T) {
	var failed []orchestration.ValidationResult
	for i := 0; i < 40; i++ {
		failed = append(failed, orchestration.ValidationResult{Name: "make check-" + fmt.Sprint(i), ExitCode: 1})
	}
	const budget = 400
	got := publicVerificationSummary("job-7", failed, budget)
	if len(got) > budget {
		t.Fatalf("summary is %d bytes, over its budget of %d:\n%s", len(got), budget, got)
	}
	if !strings.Contains(got, "audit record of job job-7.") {
		t.Errorf("the summary dropped the pointer to the output:\n%s", got)
	}
	if !strings.Contains(got, "more failed check(s)") || !strings.Contains(got, "- `make check-0` exited 1") {
		t.Errorf("the summary neither lists the first check nor counts the rest:\n%s", got)
	}
	if publicVerificationSummary("job-7", failed, 10) != "" {
		t.Error("a budget the pointer cannot fit in yields a summary; want none")
	}
	if publicVerificationSummary("job-7", nil, budget) != "" {
		t.Error("no failed checks yields a summary; want none")
	}
}

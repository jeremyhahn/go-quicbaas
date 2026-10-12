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

package orchestration

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// retiredTestKey is a real ed25519 public key from a fixed seed, rendered
// the way the KMS renders one, with a comment.
func retiredTestKey(t *testing.T, seed byte) string {
	t.Helper()
	raw := make([]byte, ed25519.SeedSize)
	for i := range raw {
		raw[i] = seed
	}
	key, err := ssh.NewPublicKey(ed25519.NewKeyFromSeed(raw).Public())
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " old-signing"
}

var retiredAt = time.Date(2026, 9, 24, 22, 15, 0, 0, time.UTC)

// TestRetiredSigningKey_ValidatesAndFingerprints: a parseable key with a
// retirement time is valid, and its fingerprint is the SHA-256 form git
// reports as %GF.
func TestRetiredSigningKey_ValidatesAndFingerprints(t *testing.T) {
	t.Parallel()
	key := RetiredSigningKey{PublicKey: retiredTestKey(t, 1), Name: "agent-signing", RetiredAt: retiredAt}
	if err := key.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	fp, err := key.Fingerprint()
	if err != nil || !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("Fingerprint = %q, %v", fp, err)
	}
	canonical, err := key.AllowedSignersKey()
	if err != nil || strings.Contains(canonical, "old-signing") || !strings.HasPrefix(canonical, "ssh-ed25519 ") {
		t.Fatalf("AllowedSignersKey = %q, %v; want the bare key with no comment", canonical, err)
	}
}

// TestRetiredSigningKey_RefusesWhatCannotBeVerifiedAgainst: an unparseable
// key, a second line smuggled after a valid one, and a key with no
// retirement time are all refused as invalid project input.
func TestRetiredSigningKey_RefusesWhatCannotBeVerifiedAgainst(t *testing.T) {
	t.Parallel()
	valid := retiredTestKey(t, 1)
	for name, key := range map[string]RetiredSigningKey{
		"garbage":         {PublicKey: "not a key", RetiredAt: retiredAt},
		"empty":           {RetiredAt: retiredAt},
		"two lines":       {PublicKey: valid + "\n" + retiredTestKey(t, 2), RetiredAt: retiredAt},
		"no retired time": {PublicKey: valid},
	} {
		t.Run(name, func(t *testing.T) {
			var invalid *ProjectInvalidError
			if err := key.Validate(); !errors.As(err, &invalid) || !errors.Is(err, ErrProjectInvalid) {
				t.Fatalf("Validate = %v, want ProjectInvalidError", err)
			}
		})
	}
}

// TestCommitSigning_RetireNeverExtendsTrust: retiring a key twice keeps the
// earlier retirement, and a new key is appended in UTC.
func TestCommitSigning_RetireNeverExtendsTrust(t *testing.T) {
	t.Parallel()
	var policy CommitSigning
	key := RetiredSigningKey{PublicKey: retiredTestKey(t, 1), RetiredAt: retiredAt}
	if err := policy.Retire(key); err != nil {
		t.Fatal(err)
	}
	later := key
	later.RetiredAt = retiredAt.Add(48 * time.Hour)
	if err := policy.Retire(later); err != nil {
		t.Fatal(err)
	}
	earlier := key
	earlier.RetiredAt = retiredAt.Add(-time.Hour)
	if err := policy.Retire(earlier); err != nil {
		t.Fatal(err)
	}
	if len(policy.Retired) != 1 || !policy.Retired[0].RetiredAt.Equal(earlier.RetiredAt) {
		t.Fatalf("Retired = %+v, want the one key at its earliest retirement", policy.Retired)
	}
	other := RetiredSigningKey{PublicKey: retiredTestKey(t, 2), RetiredAt: retiredAt.In(time.FixedZone("EDT", -4*3600))}
	if err := policy.Retire(other); err != nil {
		t.Fatal(err)
	}
	if len(policy.Retired) != 2 || policy.Retired[1].RetiredAt.Location() != time.UTC {
		t.Fatalf("Retired = %+v, want a second key recorded in UTC", policy.Retired)
	}
}

// TestCommitSigning_RetireRefusesAnInvalidKey: nothing invalid enters the
// history.
func TestCommitSigning_RetireRefusesAnInvalidKey(t *testing.T) {
	t.Parallel()
	var policy CommitSigning
	if err := policy.Retire(RetiredSigningKey{PublicKey: "nope", RetiredAt: retiredAt}); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("Retire = %v, want ErrProjectInvalid", err)
	}
	if len(policy.Retired) != 0 {
		t.Fatalf("Retired = %+v, want nothing recorded", policy.Retired)
	}
}

// TestCommitSigning_ValidateChecksTheHistory: an enabled policy with an
// invalid retired key is refused; a valid history passes.
func TestCommitSigning_ValidateChecksTheHistory(t *testing.T) {
	t.Parallel()
	policy := CommitSigning{Enabled: true, Key: KMSKeyRef{Name: "signing-kms"}}
	policy.Retired = []RetiredSigningKey{{PublicKey: retiredTestKey(t, 1), RetiredAt: retiredAt}}
	if err := policy.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	policy.Retired = append(policy.Retired, RetiredSigningKey{PublicKey: retiredTestKey(t, 2)})
	if err := policy.Validate(); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("Validate = %v, want ErrProjectInvalid for a key with no retirement", err)
	}
}

// TestAllowedSignersFor_ARetiredKeyIsValidBeforeItsRetirement: the file
// admits the current key without limit and each retired key, canonicalised,
// only before its retirement.
func TestAllowedSignersFor_ARetiredKeyIsValidBeforeItsRetirement(t *testing.T) {
	t.Parallel()
	old := RetiredSigningKey{PublicKey: retiredTestKey(t, 1), RetiredAt: retiredAt.In(time.FixedZone("EDT", -4*3600))}
	canonical, err := old.AllowedSignersKey()
	if err != nil {
		t.Fatal(err)
	}
	got := AllowedSignersFor("ssh-ed25519 AAAAcurrent agent", []RetiredSigningKey{old})
	want := `* namespaces="git" ssh-ed25519 AAAAcurrent agent` + "\n" +
		`* namespaces="git",valid-before="20260924221500Z" ` + canonical + "\n"
	if got != want {
		t.Fatalf("allowed_signers =\n%s\nwant\n%s", got, want)
	}
}

// TestAllowedSignersFor_AnInvalidRetiredKeyAdmitsNothing: a history entry
// that does not validate is left out, a smuggled second line never reaches
// the file, and with no current key there is no file at all.
func TestAllowedSignersFor_AnInvalidRetiredKeyAdmitsNothing(t *testing.T) {
	t.Parallel()
	smuggled := retiredTestKey(t, 1) + "\n* namespaces=\"git\" " + retiredTestKey(t, 9)
	got := AllowedSignersFor("ssh-ed25519 AAAAcurrent agent", []RetiredSigningKey{
		{PublicKey: "garbage", RetiredAt: retiredAt},
		{PublicKey: retiredTestKey(t, 2)},
		{PublicKey: smuggled, RetiredAt: retiredAt},
	})
	if got != `* namespaces="git" ssh-ed25519 AAAAcurrent agent`+"\n" {
		t.Fatalf("allowed_signers = %q, want only the current key", got)
	}
	if AllowedSignersFor("  ", []RetiredSigningKey{{PublicKey: retiredTestKey(t, 1), RetiredAt: retiredAt}}) != "" {
		t.Fatal("an attempt that signs nothing admits nothing, retired keys included")
	}
}

// TestWorkspaceSpecFiles_TheRetiredHistoryReachesTheAllowedSigners: the
// spec's retired keys are written into the file the container verifies with.
func TestWorkspaceSpecFiles_TheRetiredHistoryReachesTheAllowedSigners(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	spec.SigningKey = "ssh-ed25519 AAAAkey agent"
	spec.RetiredSigningKeys = []RetiredSigningKey{{PublicKey: retiredTestKey(t, 1), RetiredAt: retiredAt}}
	_, dir := writtenSpec(t, spec)
	content, err := os.ReadFile(filepath.Join(dir, WorkspaceAllowedSignersFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `valid-before="20260924221500Z"`) {
		t.Fatalf("allowed_signers = %q, want the retired key bounded by its retirement", content)
	}
}

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

package orchestration

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// GitAuthor
// =============================================================================

func TestGitAuthor_ZeroValueIsValidAndUnset(t *testing.T) {
	var author GitAuthor
	if author.Set() {
		t.Error("the zero author reports itself set")
	}
	if err := author.Validate(); err != nil {
		t.Errorf("the zero author is invalid: %v", err)
	}
}

func TestGitAuthor_HalfSetIsRefused(t *testing.T) {
	cases := map[string]struct {
		author GitAuthor
		field  string
	}{
		"no email":         {GitAuthor{Name: "agent"}, "git.author.email"},
		"no name":          {GitAuthor{Email: "agent@example.test"}, "git.author.name"},
		"whitespace name":  {GitAuthor{Name: "  ", Email: "agent@example.test"}, "git.author.name"},
		"whitespace email": {GitAuthor{Name: "agent", Email: " "}, "git.author.email"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.author.Validate()
			var invalid *ProjectInvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected *ProjectInvalidError, got %T: %v", err, err)
			}
			if invalid.Field != tc.field {
				t.Errorf("field = %q, want %q", invalid.Field, tc.field)
			}
			if !errors.Is(err, ErrProjectInvalid) {
				t.Error("error does not unwrap to ErrProjectInvalid")
			}
		})
	}
}

func TestGitAuthor_CompleteIsAccepted(t *testing.T) {
	author := GitAuthor{Name: "agent", Email: "agent@example.test"}
	if !author.Set() {
		t.Error("a complete author reports itself unset")
	}
	if err := author.Validate(); err != nil {
		t.Errorf("a complete author was refused: %v", err)
	}
}

// =============================================================================
// KMSKeyRef
// =============================================================================

func TestKMSKeyRef_Set(t *testing.T) {
	if (KMSKeyRef{}).Set() {
		t.Error("an empty ref reports itself set")
	}
	if (KMSKeyRef{Name: "  "}).Set() {
		t.Error("a whitespace name reports itself set")
	}
	if !(KMSKeyRef{Name: "signing"}).Set() {
		t.Error("a named ref reports itself unset")
	}
}

// =============================================================================
// CommitSigning
// =============================================================================

// TestCommitSigning_ZeroValueSignsNothing pins the default: an unsigned commit
// is visibly unsigned and can be re-signed; a commit signed by the wrong key
// is a provenance claim that is wrong and permanent. Signing is opted into.
func TestCommitSigning_ZeroValueSignsNothing(t *testing.T) {
	var policy CommitSigning
	if policy.Enabled {
		t.Error("the zero policy signs; signing must be opted into")
	}
	if err := policy.Validate(); err != nil {
		t.Errorf("the zero policy is invalid: %v", err)
	}
	if got := policy.SigningAlgorithm(); got != SigningAlgorithmEd25519 {
		t.Errorf("default algorithm = %q, want ed25519", got)
	}
}

// TestCommitSigning_EnabledWithoutAKeyIsRefused is the fail-closed case.
func TestCommitSigning_EnabledWithoutAKeyIsRefused(t *testing.T) {
	err := CommitSigning{Enabled: true}.Validate()
	if err == nil {
		t.Fatal("an enabled policy with no key was accepted; it would have degraded to unsigned")
	}
	var invalid *ProjectInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "git.signing.key" {
		t.Fatalf("expected git.signing.key refusal, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "never degrade to unsigned") {
		t.Errorf("error %v does not name the degradation it prevents", err)
	}
}

// TestCommitSigning_CarriesTheOwnerSAlgorithmRuling covers the default and
// the FIPS alternative, and refuses anything else.
func TestCommitSigning_CarriesTheOwnerSAlgorithmRuling(t *testing.T) {
	base := CommitSigning{Enabled: true, Key: KMSKeyRef{Name: "workspace-signing"}}

	for _, alg := range []string{"", SigningAlgorithmEd25519, SigningAlgorithmECDSAP256} {
		policy := base
		policy.Algorithm = alg
		if err := policy.Validate(); err != nil {
			t.Errorf("ruled algorithm %q was refused: %v", alg, err)
		}
	}

	policy := base
	policy.Algorithm = "RSA-2048"
	err := policy.Validate()
	var invalid *ProjectInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "git.signing.algorithm" {
		t.Fatalf("an unruled signing algorithm was accepted or misreported: %v", err)
	}
	if !strings.Contains(err.Error(), "RSA-2048") {
		t.Errorf("error %v does not name the refused algorithm", err)
	}
}

// TestCommitSigning_RoundTripsThroughJSONAndYAML pins the wire shape: the
// same policy must come back from both encodings every transport and the
// workflow files use.
func TestCommitSigning_RoundTripsThroughJSONAndYAML(t *testing.T) {
	in := ProjectGit{
		RepoURL:       "ssh://git@forge.example.test/acme/api.git",
		DefaultBranch: "main",
		Author:        GitAuthor{Name: "agent-api", Email: "agent-api@example.test"},
		Signing: CommitSigning{
			Enabled:   true,
			Algorithm: SigningAlgorithmECDSAP256,
			Key:       KMSKeyRef{Name: "api-signing", Scope: "tenant", Owner: "acme"},
		},
		SSHKey: KMSKeyRef{Name: "api-access", Scope: "service", Owner: "deploy"},
	}

	jsonBytes, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	for _, key := range []string{`"author"`, `"signing"`, `"ssh_key"`, `"enabled":true`, `"algorithm":"ECDSA-P256"`} {
		if !strings.Contains(string(jsonBytes), key) {
			t.Errorf("json %s lacks %s", jsonBytes, key)
		}
	}
	var fromJSON ProjectGit
	if err := json.Unmarshal(jsonBytes, &fromJSON); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if fromJSON != in {
		t.Errorf("json round trip changed the record:\n got %+v\nwant %+v", fromJSON, in)
	}

	yamlBytes, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}
	var fromYAML ProjectGit
	if err := yaml.Unmarshal(yamlBytes, &fromYAML); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if fromYAML != in {
		t.Errorf("yaml round trip changed the record:\n got %+v\nwant %+v", fromYAML, in)
	}

	// The zero policy must not leak keys into the wire shape: an unsigned
	// workspace says nothing about signing.
	empty, err := json.Marshal(ProjectGit{RepoURL: "https://example.test/r.git"})
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	for _, key := range []string{"author", "signing", "ssh_key"} {
		if strings.Contains(string(empty), key) {
			t.Errorf("zero-value git metadata emitted %q: %s", key, empty)
		}
	}
}

// =============================================================================
// ProjectGit.AccessKey / Validate
// =============================================================================

func TestProjectGit_AccessKeyFallsBackToTheSigningKey(t *testing.T) {
	signing := KMSKeyRef{Name: "signing", Scope: "tenant", Owner: "acme"}
	access := KMSKeyRef{Name: "access", Scope: "service", Owner: "deploy"}

	oneKey := ProjectGit{Signing: CommitSigning{Enabled: true, Key: signing}}
	if got := oneKey.AccessKey(); got != signing {
		t.Errorf("AccessKey() = %+v, want the signing key %+v", got, signing)
	}

	twoKeys := ProjectGit{Signing: CommitSigning{Enabled: true, Key: signing}, SSHKey: access}
	if got := twoKeys.AccessKey(); got != access {
		t.Errorf("AccessKey() = %+v, want the configured access key %+v", got, access)
	}

	if got := (ProjectGit{}).AccessKey(); got.Set() {
		t.Errorf("AccessKey() on a keyless workspace = %+v, want unset", got)
	}
}

func TestProjectGit_Validate(t *testing.T) {
	cases := map[string]struct {
		git   ProjectGit
		field string
	}{
		"zero value":       {ProjectGit{}, ""},
		"complete":         {ProjectGit{Author: GitAuthor{Name: "a", Email: "a@b.c"}, Signing: CommitSigning{Enabled: true, Key: KMSKeyRef{Name: "k"}}}, ""},
		"half author":      {ProjectGit{Author: GitAuthor{Name: "a"}}, "git.author.email"},
		"keyless signing":  {ProjectGit{Signing: CommitSigning{Enabled: true}}, "git.signing.key"},
		"unruled algo":     {ProjectGit{Signing: CommitSigning{Enabled: true, Key: KMSKeyRef{Name: "k"}, Algorithm: "RSA"}}, "git.signing.algorithm"},
		"disabled any alg": {ProjectGit{Signing: CommitSigning{Algorithm: "RSA"}}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.git.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("valid metadata refused: %v", err)
				}
				return
			}
			var invalid *ProjectInvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected *ProjectInvalidError, got %T: %v", err, err)
			}
			if invalid.Field != tc.field {
				t.Errorf("field = %q, want %q", invalid.Field, tc.field)
			}
		})
	}
}

// TestProject_DefaultWorkflowAndGitIdentityRoundTrip pins the project's own
// wire shape for the fields the transports carry.
func TestProject_DefaultWorkflowAndGitIdentityRoundTrip(t *testing.T) {
	in := Project{
		ID:              "p1",
		TenantID:        "acme",
		Name:            "api",
		DefaultWorkflow: "sdlc",
		Git: ProjectGit{
			Author:  GitAuthor{Name: "agent", Email: "agent@example.test"},
			Signing: CommitSigning{Enabled: true, Key: KMSKeyRef{Name: "k"}},
		},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"default_workflow":"sdlc"`) {
		t.Errorf("default_workflow missing from %s", data)
	}
	var out Project
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.DefaultWorkflow != "sdlc" || out.Git != in.Git {
		t.Errorf("round trip changed the project: %+v", out)
	}
}

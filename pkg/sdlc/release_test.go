package sdlc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const releaseSHA = "0123456789abcdef0123456789abcdef01234567"

func greenChecks(sha string) []CheckRecord {
	return []CheckRecord{{Name: "ci", HeadSHA: sha, Status: CheckSuccess}}
}

func releaseRulers() map[string]struct{} {
	return map[string]struct{}{"owner": {}}
}

func TestEvaluateReleaseGate_RefusesATagOnARedMain(t *testing.T) {
	req := ReleaseRequest{Kind: ReleaseAlpha, Tag: "v0.3.8-alpha", SHA: releaseSHA}
	cases := []struct {
		name   string
		checks []CheckRecord
	}{
		{"red", []CheckRecord{
			{Name: "build", HeadSHA: releaseSHA, Status: CheckSuccess},
			{Name: "test", HeadSHA: releaseSHA, Status: CheckFailure},
		}},
		{"running", []CheckRecord{{Name: "test", HeadSHA: releaseSHA, Status: CheckPending}}},
		{"no checks", nil},
		{"checks on another commit", greenChecks(strings.Repeat("f", 40))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateReleaseGate(req, nil, tc.checks, nil, nil)
			if got.Allowed || got.Refusal != ReleaseMainNotGreen {
				t.Fatalf("decision = %+v, want refusal %s", got, ReleaseMainNotGreen)
			}
		})
	}
}

func TestEvaluateReleaseGate_ProductionNeedsAuthorization(t *testing.T) {
	req := ReleaseRequest{Kind: ReleaseProduction, Tag: "v1.2.0", SHA: releaseSHA, ReleaseIssue: 7}
	exact := "Looks good.\nrelease-authorized: v1.2.0 " + releaseSHA + "\n"
	cases := []struct {
		name     string
		comments []ForgeComment
		allowed  bool
	}{
		{"no comment", nil, false},
		{"a non-ruler", []ForgeComment{{ID: 1, Author: "stranger", Body: exact}}, false},
		{"a ruler for another SHA", []ForgeComment{{ID: 1, Author: "owner",
			Body: "release-authorized: v1.2.0 " + strings.Repeat("a", 40)}}, false},
		{"a ruler for another tag", []ForgeComment{{ID: 1, Author: "owner",
			Body: "release-authorized: v1.2.1 " + releaseSHA}}, false},
		{"a ruler with a short SHA", []ForgeComment{{ID: 1, Author: "owner",
			Body: "release-authorized: v1.2.0 " + releaseSHA[:12]}}, false},
		{"a factory comment", []ForgeComment{{ID: 1, Author: "owner",
			Body: factoryCommentMarker + "x -->\n" + exact}}, false},
		{"an exact ruler line", []ForgeComment{{ID: 1, Author: "Owner", Body: exact}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateReleaseGate(req, nil, greenChecks(releaseSHA), tc.comments, releaseRulers())
			if got.Allowed != tc.allowed {
				t.Fatalf("decision = %+v, want allowed=%v", got, tc.allowed)
			}
			if !tc.allowed && got.Refusal != ReleaseNotAuthorized {
				t.Fatalf("refusal = %s, want %s", got.Refusal, ReleaseNotAuthorized)
			}
		})
	}
}

func TestEvaluateReleaseGate_TagShapeAndKind(t *testing.T) {
	cases := []struct {
		kind ReleaseKind
		tag  string
		sha  string
		want ReleaseRefusal
	}{
		{ReleaseAlpha, "0.1.0-alpha", releaseSHA, ReleaseTagInvalid},
		{ReleaseAlpha, "v0.1-alpha", releaseSHA, ReleaseTagInvalid},
		{ReleaseAlpha, "v01.1.0-alpha", releaseSHA, ReleaseTagInvalid},
		{ReleaseAlpha, "v0.1.0-", releaseSHA, ReleaseTagInvalid},
		{ReleaseAlpha, "v0.1.0-alpha", "0123456", ReleaseTagInvalid},
		{ReleaseAlpha, "v0.1.0", releaseSHA, ReleaseKindMismatch},
		{ReleaseAlpha, "v0.1.0-rc1", releaseSHA, ReleaseKindMismatch},
		{ReleaseStaging, "v0.1.0-alpha", releaseSHA, ReleaseKindMismatch},
		{ReleaseStaging, "v0.1.0", releaseSHA, ReleaseKindMismatch},
		{ReleaseProduction, "v0.1.0-rc1", releaseSHA, ReleaseKindMismatch},
		{ReleaseProduction, "v0.1.0-alpha", releaseSHA, ReleaseKindMismatch},
		{ReleaseKind("nightly"), "v0.1.0", releaseSHA, ReleaseKindMismatch},
		{ReleaseAlpha, "v0.1.0-alpha", releaseSHA, ""},
		{ReleaseAlpha, "v0.1.0-alpha.2", releaseSHA, ""},
		{ReleaseStaging, "v0.1.0-rc1", releaseSHA, ""},
		{ReleaseStaging, "v0.1.0-rc.2", releaseSHA, ""},
		{ReleaseStaging, "v0.1.0-staging", releaseSHA, ""},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind)+" "+tc.tag, func(t *testing.T) {
			req := ReleaseRequest{Kind: tc.kind, Tag: tc.tag, SHA: tc.sha}
			got := EvaluateReleaseGate(req, nil, greenChecks(tc.sha), nil, nil)
			if got.Refusal != tc.want || got.Allowed != (tc.want == "") {
				t.Fatalf("decision = %+v, want refusal %q", got, tc.want)
			}
		})
	}
}

func TestEvaluateReleaseGate_RefusesAnExistingTag(t *testing.T) {
	req := ReleaseRequest{Kind: ReleaseAlpha, Tag: "v0.1.0-alpha", SHA: releaseSHA}
	got := EvaluateReleaseGate(req, &ForgeTag{Name: req.Tag, SHA: releaseSHA}, greenChecks(releaseSHA), nil, nil)
	if got.Allowed || got.Refusal != ReleaseTagExists {
		t.Fatalf("decision = %+v, want refusal %s", got, ReleaseTagExists)
	}
}

func TestEvaluateReleaseGate_AlphaNeedsNoAuthorization(t *testing.T) {
	for _, req := range []ReleaseRequest{
		{Kind: ReleaseAlpha, Tag: "v0.1.0-alpha", SHA: releaseSHA},
		{Kind: ReleaseStaging, Tag: "v0.1.0-rc1", SHA: releaseSHA},
	} {
		if got := EvaluateReleaseGate(req, nil, greenChecks(releaseSHA), nil, nil); !got.Allowed {
			t.Fatalf("%s: decision = %+v, want allowed", req.Tag, got)
		}
	}
}

func TestNextAlphaTag(t *testing.T) {
	cases := map[string]string{
		"":             "v0.0.1-alpha",
		"v0.3.7-alpha": "v0.3.8-alpha",
		"v1.2.3":       "v1.2.4-alpha",
		"v1.2.9-rc1":   "v1.2.10-alpha",
	}
	for latest, want := range cases {
		got, err := NextAlphaTag(latest)
		if err != nil || got != want {
			t.Fatalf("NextAlphaTag(%q) = %q, %v; want %q", latest, got, err, want)
		}
	}
	for _, bad := range []string{"1.2.3", "v1.2", "latest"} {
		if got, err := NextAlphaTag(bad); err == nil {
			t.Fatalf("NextAlphaTag(%q) = %q, want an error", bad, got)
		}
	}
}

func TestReportConsumerCheck_ReportsAFailureOnce(t *testing.T) {
	forge := newFakeForge()
	ctx := context.Background()
	result := ConsumerCheckResult{
		Tag: "v0.3.8-alpha", SHA: releaseSHA, Consumer: "go-qrdb",
		Detail: "go build ./...\n" + strings.Repeat("x", 5000),
	}
	if err := ReportConsumerCheck(ctx, forge, testRepoRef, 7, result); err != nil {
		t.Fatalf("ReportConsumerCheck: %v", err)
	}
	if err := ReportConsumerCheck(ctx, forge, testRepoRef, 7, result); err != nil {
		t.Fatalf("second ReportConsumerCheck: %v", err)
	}
	comments := forge.commentsOn(7)
	if len(comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(comments))
	}
	body := comments[0].Body
	marker := "<!-- quicbaas-sdlc:consumer-check tag=v0.3.8-alpha consumer=go-qrdb -->"
	if !strings.HasPrefix(body, marker+"\n") {
		t.Fatalf("body does not open with the marker:\n%s", body)
	}
	for _, want := range []string{"FAILED", "v0.3.8-alpha", shortSHA(releaseSHA), "go-qrdb", "```\ngo build"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %q:\n%.300s", want, body)
		}
	}
	if len(body) > consumerCheckDetailLimit+500 {
		t.Fatalf("body is %d bytes, the detail was not limited", len(body))
	}
}

func TestReportConsumerCheck_ReturnsForgeErrors(t *testing.T) {
	ctx := context.Background()
	result := ConsumerCheckResult{Tag: "v0.3.8-alpha", SHA: releaseSHA, Consumer: "go-qrdb", Passed: true}

	listing := newFakeForge()
	listing.listCommentsErr = errors.New("forge down")
	if err := ReportConsumerCheck(ctx, listing, testRepoRef, 7, result); err == nil {
		t.Fatal("a failed listing was not returned")
	}

	posting := newFakeForge()
	posting.commentErr = errors.New("forge down")
	if err := ReportConsumerCheck(ctx, posting, testRepoRef, 7, result); err == nil {
		t.Fatal("a failed post was not returned")
	}
}

func TestReleaseGate_EvaluateReadsTheForge(t *testing.T) {
	ctx := context.Background()
	forge := newFakeForge()
	forge.putTag(testRepoRef, "v0.3.7-alpha", strings.Repeat("b", 40))
	forge.checks[releaseSHA] = []ForgeCheckRun{
		{ID: "1", Name: "test", HeadSHA: releaseSHA, Status: "completed", Conclusion: "failure"},
	}
	gate := &ReleaseGate{Now: func() time.Time { return time.Unix(0, 0) }}

	exists, err := gate.Evaluate(ctx, forge, testBinding(), testRepoRef,
		ReleaseRequest{Kind: ReleaseAlpha, Tag: "v0.3.7-alpha", SHA: releaseSHA})
	if err != nil || exists.Refusal != ReleaseTagExists {
		t.Fatalf("existing tag: decision = %+v, %v; want %s", exists, err, ReleaseTagExists)
	}

	req := ReleaseRequest{Kind: ReleaseAlpha, Tag: "v0.3.8-alpha", SHA: releaseSHA}
	red, err := gate.Evaluate(ctx, forge, testBinding(), testRepoRef, req)
	if err != nil || red.Allowed || red.Refusal != ReleaseMainNotGreen {
		t.Fatalf("red main: decision = %+v, %v; want %s", red, err, ReleaseMainNotGreen)
	}

	forge.checks[releaseSHA] = []ForgeCheckRun{
		{ID: "2", Name: "test", HeadSHA: releaseSHA, Status: "completed", Conclusion: "success"},
	}
	green, err := gate.Evaluate(ctx, forge, testBinding(), testRepoRef, req)
	if err != nil || !green.Allowed {
		t.Fatalf("green main: decision = %+v, %v; want allowed", green, err)
	}

	prod := ReleaseRequest{Kind: ReleaseProduction, Tag: "v0.4.0", SHA: releaseSHA, ReleaseIssue: 9}
	if got, err := gate.Evaluate(ctx, forge, testBinding(), testRepoRef, prod); err != nil || got.Refusal != ReleaseNotAuthorized {
		t.Fatalf("unauthorized production: decision = %+v, %v", got, err)
	}
	forge.comments[9] = []ForgeComment{{ID: 1, Author: testBinding().Forge.Owner,
		Body: "release-authorized: v0.4.0 " + releaseSHA}}
	if got, err := gate.Evaluate(ctx, forge, testBinding(), testRepoRef, prod); err != nil || !got.Allowed {
		t.Fatalf("authorized production: decision = %+v, %v", got, err)
	}

	forge.getTagErr = errors.New("forge down")
	if _, err := gate.Evaluate(ctx, forge, testBinding(), testRepoRef, req); err == nil {
		t.Fatal("a failed tag read was not returned")
	}
}

package sdlc

import (
	"testing"
	"time"
)

func TestEvaluateResponseDeadlines(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	check := func(name, head string, status CheckStatus, finished time.Duration) CheckRecord {
		return CheckRecord{Name: name, HeadSHA: head, Status: status, FinishedAt: ago(finished)}
	}
	review := func(reviewer string, verdict ReviewVerdict, head string, submitted time.Duration) ReviewRecord {
		return ReviewRecord{Reviewer: reviewer, Verdict: verdict, HeadSHA: head, SubmittedAt: ago(submitted)}
	}

	cases := map[string]struct {
		checks  []CheckRecord
		reviews []ReviewRecord
		want    []ResponseDue
	}{
		"green": {checks: []CheckRecord{check("ci", "h", CheckSuccess, 3*time.Hour)}},
		"red 59m": {
			checks: []CheckRecord{check("build", "h", CheckSuccess, 2*time.Hour), check("test", "h", CheckFailure, 59*time.Minute)},
			want:   []ResponseDue{{Kind: ResponseDueRed, HeadSHA: "h", Since: ago(59 * time.Minute)}},
		},
		"red 61m": {
			checks: []CheckRecord{check("test", "h", CheckFailure, 61*time.Minute)},
			want:   []ResponseDue{{Kind: ResponseDueRed, HeadSHA: "h", Since: ago(61 * time.Minute), Overdue: true}},
		},
		"red on an old head": {
			checks:  []CheckRecord{check("test", "old", CheckFailure, 5*time.Hour)},
			reviews: []ReviewRecord{review("codex", VerdictRequestChanges, "old", 5*time.Hour)},
		},
		"cancelled only": {checks: []CheckRecord{check("test", "h", CheckCancelled, 5*time.Hour)}},
		"changes 1h": {
			reviews: []ReviewRecord{review("codex", VerdictRequestChanges, "h", time.Hour)},
			want:    []ResponseDue{{Kind: ResponseDueChangesRequested, HeadSHA: "h", Since: ago(time.Hour)}},
		},
		"changes 3h": {
			reviews: []ReviewRecord{review("codex", VerdictRequestChanges, "h", 3*time.Hour)},
			want:    []ResponseDue{{Kind: ResponseDueChangesRequested, HeadSHA: "h", Since: ago(3 * time.Hour), Overdue: true}},
		},
		"changes 25h": {
			reviews: []ReviewRecord{review("codex", VerdictRequestChanges, "h", 25*time.Hour)},
			want:    []ResponseDue{{Kind: ResponseDueChangesRequested, HeadSHA: "h", Since: ago(25 * time.Hour), Overdue: true, Defect: true}},
		},
		"changes superseded by a later approve": {
			reviews: []ReviewRecord{review("codex", VerdictRequestChanges, "h", 5*time.Hour), review("codex", VerdictApprove, "h", time.Hour)},
		},
		"red then changes": {
			checks:  []CheckRecord{check("test", "h", CheckFailure, 2*time.Hour)},
			reviews: []ReviewRecord{review("codex", VerdictRequestChanges, "h", time.Hour)},
			want: []ResponseDue{
				{Kind: ResponseDueRed, HeadSHA: "h", Since: ago(2 * time.Hour), Overdue: true},
				{Kind: ResponseDueChangesRequested, HeadSHA: "h", Since: ago(time.Hour)},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := EvaluateResponseDeadlines("h", tc.checks, tc.reviews, now)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %+v, want nil", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if got[i].Kind != tc.want[i].Kind || got[i].HeadSHA != tc.want[i].HeadSHA || !got[i].Since.Equal(tc.want[i].Since) ||
					got[i].Overdue != tc.want[i].Overdue || got[i].Defect != tc.want[i].Defect {
					t.Fatalf("[%d] got %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

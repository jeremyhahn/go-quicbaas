package sdlc

import "time"

// Response deadlines for the author of a pull request. A settled red check
// and a request for changes on the current head both wait on the author; a
// newer head is the response, so records for any other head never count.
const (
	// RedResponseDeadline is how long a settled red check on the head may
	// stand before the response to it is overdue.
	RedResponseDeadline = time.Hour

	// ChangesResponseDeadline is how long a request for changes on the head
	// may stand before the response to it is overdue.
	ChangesResponseDeadline = 2 * time.Hour

	// ChangesDefectDeadline is how long a request for changes on the head may
	// stand before the missing response is a defect.
	ChangesDefectDeadline = 24 * time.Hour
)

// Kinds of ResponseDue.
const (
	ResponseDueRed              = "red"
	ResponseDueChangesRequested = "changes_requested"
)

// ResponseDue is one response the author owes on a head, and how late it is.
type ResponseDue struct {
	// Kind is ResponseDueRed or ResponseDueChangesRequested.
	Kind string
	// HeadSHA is the head the red or the request for changes applies to.
	HeadSHA string
	// Since is when the response became due: the latest failed check's
	// finish, or the latest request for changes' submission.
	Since time.Time
	// Overdue is set once the kind's response deadline has passed.
	Overdue bool
	// Defect is set once a request for changes has stood past
	// ChangesDefectDeadline.
	Defect bool
}

// EvaluateResponseDeadlines returns the responses owed on headSHA at now, red
// first and then changes requested, or nil when none is owed. It is pure: it
// reads only the records it is given.
func EvaluateResponseDeadlines(headSHA string, checks []CheckRecord, reviews []ReviewRecord, now time.Time) []ResponseDue {
	if headSHA == "" {
		return nil
	}
	var due []ResponseDue

	var redSince time.Time
	for _, check := range checks {
		if check.HeadSHA != headSHA || !check.Status.Finished() || check.FinishedAt.IsZero() {
			continue
		}
		// A cancelled run is void, not red.
		if check.Status == CheckCancelled || !checkFailed(check.Status) {
			continue
		}
		if check.FinishedAt.After(redSince) {
			redSince = check.FinishedAt
		}
	}
	if !redSince.IsZero() {
		due = append(due, ResponseDue{
			Kind:    ResponseDueRed,
			HeadSHA: headSHA,
			Since:   redSince,
			Overdue: now.Sub(redSince) > RedResponseDeadline,
		})
	}

	var changesSince time.Time
	found := false
	for _, review := range latestVerdictsForHead(reviews, headSHA) {
		if review.Verdict != VerdictRequestChanges {
			continue
		}
		if !found || review.SubmittedAt.After(changesSince) {
			changesSince = review.SubmittedAt
			found = true
		}
	}
	if found {
		age := now.Sub(changesSince)
		due = append(due, ResponseDue{
			Kind:    ResponseDueChangesRequested,
			HeadSHA: headSHA,
			Since:   changesSince,
			Overdue: age > ChangesResponseDeadline,
			Defect:  age > ChangesDefectDeadline,
		})
	}
	return due
}

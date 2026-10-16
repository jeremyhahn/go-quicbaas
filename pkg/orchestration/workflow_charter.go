package orchestration

import (
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// Workflow role charters
// =============================================================================
//
// A charter is the standing instruction set a workflow role's session starts
// with. Each is a prompt of the workflow set, so it is versioned, validated
// and overridable per tenant and project like every other prompt, and a job
// renders it from the set it resolved at launch (JobRun.Prompts).
//
// The charter is chosen by what the session DOES, not by the role's name in
// the workflow definition: a workflow may call its workers "coder" and
// "tester", and both are spawned with the worker charter.

// The charters, one per session the control loop starts.
const (
	charterLead      = "charter_lead"
	charterWorker    = "charter_worker"
	charterReviewer  = "charter_reviewer"
	charterValidator = "charter_validator"
)

// ErrCharterRender indicates a role charter could not be rendered.
var ErrCharterRender = errors.New("orchestration: role charter could not be rendered")

// CharterRenderError is a charter the job's prompt set could not render. The
// session it was for is not started: a session without its charter does not
// know the reply its caller parses.
type CharterRenderError struct {
	Charter string
	Cause   error
}

// Error implements the error interface.
func (e *CharterRenderError) Error() string {
	return ErrCharterRender.Error() + ": " + e.Charter + ": " + e.Cause.Error()
}

// Unwrap returns both the sentinel and the cause for errors.Is/As support.
func (e *CharterRenderError) Unwrap() []error {
	return []error{ErrCharterRender, e.Cause}
}

// renderCharter renders one charter from set.
func renderCharter(set prompts.Renderer, charter string) (string, error) {
	rendered, err := set.Render(charter, nil)
	if err != nil {
		return "", &CharterRenderError{Charter: charter, Cause: err}
	}
	return rendered, nil
}

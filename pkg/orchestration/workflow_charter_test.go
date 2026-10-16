package orchestration

import (
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// errRenderBroken is the failure failingRenderer reports.
var errRenderBroken = errors.New("render broken")

// failingRenderer is a prompt set none of whose prompts render.
type failingRenderer struct{}

func (failingRenderer) Render(string, prompts.Vars) (string, error) { return "", errRenderBroken }

func (failingRenderer) MustRender(string, prompts.Vars) string { panic(errRenderBroken) }

func TestRenderCharter_RendersEveryCharter(t *testing.T) {
	for _, charter := range []string{charterLead, charterWorker, charterReviewer, charterValidator} {
		rendered, err := renderCharter(workflowPrompts, charter)
		if err != nil {
			t.Fatalf("%s: %v", charter, err)
		}
		if strings.TrimSpace(rendered) == "" {
			t.Fatalf("%s rendered empty", charter)
		}
	}
}

func TestRenderCharter_ParsedRepliesAreInstructed(t *testing.T) {
	// The reviewer's and the validator's replies are parsed as JSON; their
	// charters must name every key the parser reads.
	wantKeys := map[string][]string{
		charterReviewer:  {`"approved"`, `"feedback"`, `"new_work_items"`},
		charterValidator: {`"passed"`, `"message"`},
	}
	for charter, keys := range wantKeys {
		rendered := mustRenderCharter(t, charter)
		for _, key := range keys {
			if !strings.Contains(rendered, key) {
				t.Fatalf("%s does not instruct the key %s: %q", charter, key, rendered)
			}
		}
	}
}

func TestRenderCharter_FailureIsTyped(t *testing.T) {
	rendered, err := renderCharter(failingRenderer{}, charterWorker)

	if rendered != "" {
		t.Fatalf("a failed render must not yield a prompt, got %q", rendered)
	}
	if !errors.Is(err, ErrCharterRender) || !errors.Is(err, errRenderBroken) {
		t.Fatalf("err = %v, want ErrCharterRender wrapping the cause", err)
	}
	var charterErr *CharterRenderError
	if !errors.As(err, &charterErr) || charterErr.Charter != charterWorker {
		t.Fatalf("err = %#v, want a CharterRenderError naming %s", err, charterWorker)
	}
}

func TestRenderCharter_UnknownCharterFails(t *testing.T) {
	_, err := renderCharter(workflowPrompts, "charter_nobody")

	if !errors.Is(err, ErrCharterRender) {
		t.Fatalf("err = %v, want ErrCharterRender", err)
	}
}

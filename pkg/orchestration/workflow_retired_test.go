package orchestration

import (
	"errors"
	"testing"
)

func TestRefuseRetiredWorkflowFields_RefusesJSONSystemPrompt(t *testing.T) {
	doc := []byte(`{"name":"w","agents":[{"role":"lead"},{"role":"worker","system_prompt":"be careful"}]}`)

	err := RefuseRetiredWorkflowFields("request", doc)

	var retired *RetiredFieldError
	if !errors.Is(err, ErrWorkflowRetiredField) || !errors.As(err, &retired) {
		t.Fatalf("err = %v, want a RetiredFieldError", err)
	}
	if retired.Path != "request" || retired.Field != "agents[1].system_prompt" {
		t.Fatalf("refusal = %+v, want request agents[1].system_prompt", retired)
	}
}

func TestRefuseRetiredWorkflowFields_AcceptsCleanAndRejectsMalformed(t *testing.T) {
	if err := RefuseRetiredWorkflowFields("request", []byte(`{"agents":[{"role":"worker","prompt_append":"x"}]}`)); err != nil {
		t.Fatalf("clean definition refused: %v", err)
	}
	if err := RefuseRetiredWorkflowFields("request", []byte("{{not yaml:::")); !errors.Is(err, ErrWorkflowParse) {
		t.Fatalf("malformed err = %v, want ErrWorkflowParse", err)
	}
}

func TestRetiredAgentFieldError_OnlyForRetiredFields(t *testing.T) {
	if err := RetiredAgentFieldError("grpc", 0, "prompt_append"); err != nil {
		t.Fatalf("a live field must not be refused: %v", err)
	}
	if err := RetiredAgentFieldError("grpc", 2, "system_prompt"); !errors.Is(err, ErrWorkflowRetiredField) {
		t.Fatalf("err = %v, want ErrWorkflowRetiredField", err)
	}
}

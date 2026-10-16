package orchestration

import (
	"strconv"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// Retired workflow-definition fields
// =============================================================================
//
// A retired field is refused wherever a definition enters, never dropped: the
// typed decode ignores an unknown key without a word, so a caller still
// setting one would believe it took effect. Stored definitions are not
// checked; only what a caller sends is.

// retiredAgentFields maps each role-spec key a workflow definition may no
// longer carry to where what it held lives now.
var retiredAgentFields = map[string]string{
	"system_prompt": "a role's standing instructions are the charter_<role> prompts of the " +
		string(SetWorkflow) + " set in the prompts store; edit them with `quicbaas prompts`",
}

// RefuseRetiredWorkflowFields returns a RetiredFieldError for the first agent
// of definition, a raw YAML or JSON workflow definition, that sets a retired
// field. source names where the definition came from (a file path, a request).
func RefuseRetiredWorkflowFields(source string, definition []byte) error {
	var raw struct {
		Agents []map[string]any `yaml:"agents"`
	}
	if err := yaml.Unmarshal(definition, &raw); err != nil {
		return &WorkflowParseError{Path: source, Cause: err}
	}
	for i, agent := range raw.Agents {
		for field := range retiredAgentFields {
			if _, present := agent[field]; present {
				return RetiredAgentFieldError(source, i, field)
			}
		}
	}
	return nil
}

// RetiredAgentFieldError is the refusal of the retired field set by agent
// index of the definition from source. A field that is not retired yields nil.
func RetiredAgentFieldError(source string, index int, field string) error {
	successor, retired := retiredAgentFields[field]
	if !retired {
		return nil
	}
	return &RetiredFieldError{
		Path:      source,
		Field:     "agents[" + strconv.Itoa(index) + "]." + field,
		Successor: successor,
	}
}

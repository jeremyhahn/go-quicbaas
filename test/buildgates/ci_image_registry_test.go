package buildgates

import (
	"fmt"
	"strings"
	"testing"
)

// registryEnvRead marks a step that hands the image registry to its script.
const registryEnvRead = "CI_IMAGE_REGISTRY: ${{"

// registrySecretLine is the one form a read of the image registry may take:
// the repository secret, which the runner masks in the log, and which a
// GitHub-hosted runner does not receive.
const registrySecretLine = "CI_IMAGE_REGISTRY: ${{ runner.environment != 'github-hosted' && secrets.CI_IMAGE_REGISTRY || '' }}"

// registryVarRead is the registry read from an Actions variable, which the log
// prints unmasked. It is assembled so no file holds the forbidden form.
const registryVarRead = "vars" + ".CI_IMAGE_REGISTRY"

// registryCacheSteps is the pull and push registry-cache steps in the three
// jobs that build the toolchain image.
const registryCacheSteps = 6

const (
	registryPullStep = "Pull the toolchain image from the registry cache"
	registryPushStep = "Push the toolchain image to the registry cache"
)

// registryStepProblems lists what is wrong with how ci.yml's steps read the
// image registry. Every step that reads it must take it from the secret in
// the runner-gated form, and the six registry-cache steps must all be there.
func registryStepProblems(body string) []string {
	var problems []string
	if strings.Contains(body, registryVarRead) {
		problems = append(problems, "the workflow reads the image registry from an Actions variable, which the log prints unmasked")
	}
	jobs := body
	if i := strings.Index(body, "\njobs:\n"); i >= 0 {
		jobs = body[i:]
	}
	cacheSteps := 0
	for _, step := range ciSteps(jobs) {
		name := stepName(step)
		if strings.Contains(step, registryEnvRead) && !hasTrimmedLine(step, registrySecretLine) {
			problems = append(problems, fmt.Sprintf("step %q reads the image registry without the line %q", name, registrySecretLine))
		}
		if name != registryPullStep && name != registryPushStep {
			continue
		}
		cacheSteps++
		if !strings.Contains(step, registrySecretLine) {
			problems = append(problems, fmt.Sprintf("registry-cache step %q lacks the line %q", name, registrySecretLine))
		}
	}
	if cacheSteps != registryCacheSteps {
		problems = append(problems, fmt.Sprintf("%d registry-cache steps, want %d", cacheSteps, registryCacheSteps))
	}
	return problems
}

// hasTrimmedLine reports whether one of text's lines, trimmed, equals want.
func hasTrimmedLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func TestWorkflowsReadTheImageRegistryFromASecret(t *testing.T) {
	for _, p := range registryStepProblems(readFile(t, ciWorkflowPath)) {
		t.Errorf("%s: %s", ciWorkflowPath, p)
	}
	for _, path := range workflowFiles(t) {
		if strings.Contains(readFile(t, path), registryVarRead) {
			t.Errorf("%s reads the image registry from an Actions variable; it is an Actions secret", path)
		}
	}
}

func TestRegistryStepProblemsCatchesAVariableRead(t *testing.T) {
	body := readFile(t, ciWorkflowPath)
	cases := map[string]string{
		"variable read": strings.Replace(body, "secrets.CI_IMAGE_REGISTRY ||", registryVarRead+" ||", 1),
		"hosted runner gets the secret": strings.Replace(body,
			"runner.environment != 'github-hosted' && secrets.CI_IMAGE_REGISTRY || ''", "secrets.CI_IMAGE_REGISTRY", 1),
		"cache step dropped": strings.Replace(body,
			"name: "+registryPullStep, "name: Pull the toolchain image", 1),
	}
	for name, mutated := range cases {
		if mutated == body {
			t.Fatalf("%s: the mutation changed nothing; the fixture text has moved", name)
		}
		if len(registryStepProblems(mutated)) == 0 {
			t.Errorf("%s: registryStepProblems found nothing wrong", name)
		}
	}
}

package buildgates

import (
	"fmt"
	"strings"
	"testing"
)

// registryEnvRead marks a step that hands the image registry to its script.
const registryEnvRead = "CI_IMAGE_REGISTRY: ${{"

// registrySecretLine is the one form that read may take: the registry is a
// repository secret, so the log masks it.
const registrySecretLine = "CI_IMAGE_REGISTRY: ${{ secrets.CI_IMAGE_REGISTRY }}"

// registryVarRead is the registry read from an Actions variable, which the log
// prints unmasked. It is assembled so no file holds the forbidden form.
const registryVarRead = "vars" + ".CI_IMAGE_REGISTRY"

// registryStepCount is the registry cache steps in ci.yml: a pull and a push
// in each of the three jobs that build the toolchain image.
const registryStepCount = 6

// registryStepProblems lists what is wrong with how ci.yml's steps read the
// image registry. Every step that reads it must take it from the secret,
// fail when it is empty, and mask it before the cache script runs.
func registryStepProblems(body string) []string {
	var problems []string
	if strings.Contains(body, registryVarRead) {
		problems = append(problems, "the workflow reads the image registry from an Actions variable, which the log prints unmasked")
	}
	jobs := body
	if i := strings.Index(body, "\njobs:\n"); i >= 0 {
		jobs = body[i:]
	}
	count := 0
	for _, step := range ciSteps(jobs) {
		if !strings.Contains(step, registryEnvRead) {
			continue
		}
		count++
		name := stepName(step)
		hasSecret := false
		for _, line := range strings.Split(step, "\n") {
			if strings.TrimSpace(line) == registrySecretLine {
				hasSecret = true
			}
		}
		if !hasSecret {
			problems = append(problems, fmt.Sprintf("step %q lacks the line %q", name, registrySecretLine))
		}
		check := strings.Index(step, `if [ -z "$CI_IMAGE_REGISTRY" ]`)
		if check < 0 {
			problems = append(problems, fmt.Sprintf("step %q does not check for an empty registry", name))
			continue
		}
		exit := strings.Index(step[check:], "exit 1")
		if exit < 0 {
			problems = append(problems, fmt.Sprintf("step %q does not fail (exit 1) on an empty registry", name))
			continue
		}
		exit += check
		mask := strings.Index(step, `echo "::add-mask::$CI_IMAGE_REGISTRY"`)
		if mask < 0 {
			problems = append(problems, fmt.Sprintf("step %q does not mask the registry", name))
			continue
		}
		if mask < exit {
			problems = append(problems, fmt.Sprintf("step %q masks the registry before it fails on an empty one", name))
		}
		if script := strings.Index(step, "ci-image-cache.sh"); script >= 0 && script < mask {
			problems = append(problems, fmt.Sprintf("step %q runs the cache script before it masks the registry", name))
		}
	}
	if count != registryStepCount {
		problems = append(problems, fmt.Sprintf("%d steps read the image registry, want %d", count, registryStepCount))
	}
	return problems
}

func TestWorkflowsReadTheImageRegistryFromASecret(t *testing.T) {
	for _, p := range registryStepProblems(readFile(t, ciWorkflowPath)) {
		t.Errorf("%s: %s", ciWorkflowPath, p)
	}
	for _, path := range workflowFiles(t) {
		if strings.Contains(readFile(t, path), registryVarRead) {
			t.Errorf("%s reads the image registry from an Actions variable; it is a required, masked secret", path)
		}
	}
}

func TestRegistryStepProblemsCatchesAVariableRead(t *testing.T) {
	body := readFile(t, ciWorkflowPath)
	cases := map[string]string{
		"variable read": strings.Replace(body, "secrets.CI_IMAGE_REGISTRY }}", registryVarRead+" }}", 1),
		"mask removed":  strings.Replace(body, `echo "::add-mask::$CI_IMAGE_REGISTRY"`+"\n", "", 1),
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

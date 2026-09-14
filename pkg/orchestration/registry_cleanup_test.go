package orchestration

import (
	"errors"
	"testing"
)

// =============================================================================
// Test-scoped registration into the engine registries
// =============================================================================
//
// The engine registries — runners, hook sets, validators and task sources —
// are process-global and append-only: a name registers once and a second
// registration under the same name is refused, so two modules cannot
// silently shadow each other. That is the right rule for a daemon, whose
// modules register once at startup, and the wrong one for a test binary,
// where `go test -count=N` runs every test in ONE process: with no undo,
// the second iteration's registration is a duplicate and the test that owns
// the name fails.
//
// These helpers register the way a module does and remove the entry when the
// test that owns it ends, so the package is idempotent under -count=N and a
// name is visible only while its test runs. They reach the registry's map
// directly rather than through an exported Unregister because unregistration
// is a property of this test binary, not of the engine's API: production
// registration stays append-only.

// registerJobRunnerForTest registers runner in the engine's runner registry
// for the duration of t.
func registerJobRunnerForTest(t *testing.T, runner JobRunner) {
	t.Helper()
	if err := RegisterJobRunner(runner); err != nil {
		t.Fatalf("RegisterJobRunner: %v", err)
	}
	name := runner.Name()
	t.Cleanup(func() {
		jobRunners.mu.Lock()
		defer jobRunners.mu.Unlock()
		delete(jobRunners.byName, name)
	})
}

// registerJobHooksForTest registers hooks in the engine's hook registry
// under name for the duration of t.
func registerJobHooksForTest(t *testing.T, name string, hooks JobLifecycleHooks) {
	t.Helper()
	if err := RegisterJobHooks(name, hooks); err != nil {
		t.Fatalf("RegisterJobHooks: %v", err)
	}
	t.Cleanup(func() {
		jobHooks.mu.Lock()
		defer jobHooks.mu.Unlock()
		delete(jobHooks.byName, name)
	})
}

// registerValidatorForTest registers fn in the engine's validator registry
// under kind for the duration of t.
func registerValidatorForTest(t *testing.T, kind string, fn ValidatorFunc) {
	t.Helper()
	if err := RegisterValidator(kind, fn); err != nil {
		t.Fatalf("RegisterValidator: %v", err)
	}
	t.Cleanup(func() {
		validators.mu.Lock()
		defer validators.mu.Unlock()
		delete(validators.validators, kind)
	})
}

// registerTaskSourceForTest registers src in the engine's task-source
// registry, declaring kind, for the duration of t.
func registerTaskSourceForTest(t *testing.T, kind TaskSourceKind, src TaskSource) {
	t.Helper()
	if err := RegisterTaskSource(kind, src); err != nil {
		t.Fatalf("RegisterTaskSource: %v", err)
	}
	name := src.Name()
	t.Cleanup(func() { unregisterTaskSource(name) })
}

// unregisterTaskSource removes the source registered under name and, when it
// was the last source declaring its kind, the kind it declared: a kind is
// valid engine-wide only while some registered source declares it, so a
// declaration has to leave with the source that made it. The kind set is
// taken under the registry lock, the order Register uses.
func unregisterTaskSource(name string) {
	taskSources.mu.Lock()
	defer taskSources.mu.Unlock()
	reg, registered := taskSources.byName[name]
	if !registered {
		return
	}
	delete(taskSources.byName, name)
	for _, other := range taskSources.byName {
		if other.Kind == reg.Kind {
			return
		}
	}
	taskSourceKinds.mu.Lock()
	defer taskSourceKinds.mu.Unlock()
	delete(taskSourceKinds.kinds, reg.Kind)
}

// TestRegistrationCleanup_ReleasesEveryNameWhenTheTestEnds proves the
// helpers undo what they register: two rounds register the same names in
// turn, which only succeeds if the first round's cleanup ran, and nothing
// the rounds registered is visible afterwards. It is exactly the property
// `go test -count=N` needs from every registering test in the package.
func TestRegistrationCleanup_ReleasesEveryNameWhenTheTestEnds(t *testing.T) {
	const name = "registry-cleanup-test"
	const kind TaskSourceKind = "registry-cleanup-test-kind"

	for _, round := range []string{"first", "second"} {
		t.Run(round, func(t *testing.T) {
			registerJobRunnerForTest(t, &fakeRunner{name: name})
			registerJobHooksForTest(t, name, hooksTestFake{})
			registerValidatorForTest(t, name, fixedVerdict(true, ""))
			registerTaskSourceForTest(t, kind, &fakeTaskSource{name: name})

			if _, ok := LookupJobRunner(name); !ok {
				t.Error("the runner must be registered while its test runs")
			}
			if _, ok := LookupJobHooks(name); !ok {
				t.Error("the hook set must be registered while its test runs")
			}
			if err := ValidateValidatorKind(name); err != nil {
				t.Errorf("the validator kind must validate while its test runs: %v", err)
			}
			if _, ok := LookupTaskSource(name); !ok {
				t.Error("the task source must be registered while its test runs")
			}
		})
	}

	if _, ok := LookupJobRunner(name); ok {
		t.Error("the runner must leave with the test that registered it")
	}
	if _, ok := LookupJobHooks(name); ok {
		t.Error("the hook set must leave with the test that registered it")
	}
	if err := ValidateValidatorKind(name); !errors.Is(err, ErrWorkflowValidation) {
		t.Errorf("the validator kind must leave with the test that registered it, got %v", err)
	}
	if _, ok := LookupTaskSource(name); ok {
		t.Error("the task source must leave with the test that registered it")
	}
	var invalid *InvalidTaskSourceKindError
	if err := kind.Validate(); !errors.As(err, &invalid) {
		t.Errorf("the declared kind must leave with the source that declared it, got %v", err)
	}
}

// TestUnregisterTaskSource_KeepsAKindTwoSourcesShare proves the two ways
// unregistration must not overreach: a name nothing registered is a no-op,
// and a kind a second registered source still declares stays valid — only
// the last declaration takes the kind with it.
func TestUnregisterTaskSource_KeepsAKindTwoSourcesShare(t *testing.T) {
	const kind TaskSourceKind = "registry-cleanup-shared-kind"

	unregisterTaskSource("registry-cleanup-nobody-registered-this")

	registerTaskSourceForTest(t, kind, &fakeTaskSource{name: "registry-cleanup-shared-a"})
	registerTaskSourceForTest(t, kind, &fakeTaskSource{name: "registry-cleanup-shared-b"})

	unregisterTaskSource("registry-cleanup-shared-a")
	if _, ok := LookupTaskSource("registry-cleanup-shared-b"); !ok {
		t.Fatal("unregistering one source must leave the other registered")
	}
	if err := kind.Validate(); err != nil {
		t.Fatalf("a kind the remaining source declares stays valid: %v", err)
	}
}

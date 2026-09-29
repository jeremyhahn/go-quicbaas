package orchestration

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestElideHeadKeepsShortTextWhole(t *testing.T) {
	text := "ok  \tgithub.com/x/y\t0.1s"
	if got := elideHead(text, len(text)); got != text {
		t.Fatalf("short text changed: %q", got)
	}
}

func TestElideHeadKeepsTheTail(t *testing.T) {
	text := strings.Repeat("a", 3*MaxValidationTextBytes) + "FAIL: TestLast"
	got := elideHead(text, len(text))

	if !strings.HasSuffix(got, "FAIL: TestLast") {
		t.Fatal("the tail naming the failure was dropped")
	}
	if want := "[… " + strconv.Itoa(len(text)-MaxValidationTextBytes) + " earlier bytes elided]\n"; !strings.HasPrefix(got, want) {
		t.Fatalf("marker = %q, want prefix %q", got[:40], want)
	}
}

func TestElideHeadNeverSplitsARune(t *testing.T) {
	// A multi-byte rune straddling the cut must be dropped whole.
	text := strings.Repeat("é", MaxValidationTextBytes) // 2 bytes each
	got := elideHead(text, len(text))
	if !utf8.ValidString(got) {
		t.Fatal("elided text is not valid UTF-8")
	}
}

func TestTailWriterKeepsOnlyTheTail(t *testing.T) {
	var w tailWriter
	chunk := strings.Repeat("x", 1000) + "\n"
	for range 200 {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if _, err := w.Write([]byte("--- FAIL: TestEnd")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if len(w.buf) > 2*MaxValidationTextBytes {
		t.Fatalf("buffer grew to %d bytes", len(w.buf))
	}
	got := w.String()
	if !strings.HasSuffix(got, "--- FAIL: TestEnd") || !strings.Contains(got, "earlier bytes elided]") {
		t.Fatalf("tail or marker missing: %q…", got[:60])
	}
}

func TestBoundVerificationReportsNothingForSmallResults(t *testing.T) {
	status := &JobStatus{Verification: []ValidationResult{{Name: "make build", Passed: true, Message: "ok"}}}
	if status.boundVerification() {
		t.Fatal("a small result was reported as cut")
	}
}

func TestJobRunVerifyRecordsBoundedResults(t *testing.T) {
	registry := NewValidatorRegistry()
	huge := strings.Repeat("=== RUN\n", MaxValidationTextBytes)
	if err := registry.Register("huge", func(context.Context, *JobContext, ReviewCriterion) (ValidationResult, error) {
		return ValidationResult{Name: "make test", Passed: true, Message: huge}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	run := &JobRun{validatorRegistry: registry}

	results, _, err := run.Verify(context.Background(), []ReviewCriterion{{Name: "make test", Validator: "huge", Required: true}})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	recorded := run.verificationResults()
	if len(recorded) != 1 || len(recorded[0].Message) > MaxValidationTextBytes+64 {
		t.Fatalf("recorded message is %d bytes, want the bounded tail", len(recorded[0].Message))
	}
	if len(results[0].Message) > MaxValidationTextBytes+64 {
		t.Fatalf("returned message is %d bytes; runners quote it to the agent", len(results[0].Message))
	}
}

func TestRecoverJobsCompactsOversizedVerification(t *testing.T) {
	kv := newMockKVStore()
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	status := &JobStatus{
		JobID:    "job-big",
		TenantID: "tenant-1",
		State:    JobStateCompleted,
		Verification: []ValidationResult{
			{Name: "make test", Passed: true, Message: strings.Repeat("ok\n", 3*MaxValidationTextBytes)},
		},
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	key := jobPersistenceKey(status.TenantID, status.JobID)
	if err := kv.Put(context.Background(), key, data); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := svc.recoverJobs(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}

	stored, err := kv.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var loaded JobStatus
	if err := json.Unmarshal(stored, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(loaded.Verification[0].Message) > MaxValidationTextBytes+64 {
		t.Fatalf("record still carries %d bytes of output", len(loaded.Verification[0].Message))
	}
	if loaded.State != JobStateCompleted {
		t.Fatalf("compaction changed the state to %q", loaded.State)
	}
}

func TestAddFeedbackKeepsTheTail(t *testing.T) {
	memory := NewJobMemory()
	memory.AddFeedback(strings.Repeat("go: downloading x\n", MaxFeedbackBytes) + "--- FAIL: TestLast")

	got := memory.ReviewFeedback[0]
	if len(got) > MaxFeedbackBytes+64 || !strings.HasSuffix(got, "--- FAIL: TestLast") {
		t.Fatalf("feedback is %d bytes, want the bounded tail ending in the failure", len(got))
	}
}

func TestBoundVerificationCompactsOldFeedback(t *testing.T) {
	status := &JobStatus{Memory: &JobMemory{ReviewFeedback: []string{"short", strings.Repeat("y", 2*MaxFeedbackBytes)}}}

	if !status.boundVerification() {
		t.Fatal("oversized feedback was not reported")
	}
	if status.Memory.ReviewFeedback[0] != "short" || len(status.Memory.ReviewFeedback[1]) > MaxFeedbackBytes+64 {
		t.Fatal("feedback was not cut to its tail, or a short entry changed")
	}
}

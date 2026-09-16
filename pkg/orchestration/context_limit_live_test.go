//go:build openvino_live

package orchestration

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// TestContextLimit_LearnsFromARealServer drives a real OpenAI-compatible
// endpoint: one oversized prompt must come back as PromptTooLongError and
// leave the ceiling narrowed, and the next oversized prompt must be refused
// locally without a round trip.
func TestContextLimit_LearnsFromARealServer(t *testing.T) {
	base, model := os.Getenv("OPENVINO_BASE_URL"), os.Getenv("OPENVINO_MODEL")
	if base == "" || model == "" {
		t.Skip("OPENVINO_BASE_URL and OPENVINO_MODEL are required")
	}
	b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: base, DefaultModel: model, RequestTimeout: 5 * time.Minute})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	huge := strings.Repeat("word ", 60000)
	_, err := b.complete(ctx, ovRequest{Model: model, Messages: []ovMessage{{Role: "user", Content: huge}}, MaxTokens: 4})
	var tooLong *PromptTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("an oversized prompt did not produce PromptTooLongError: %v", err)
	}
	t.Logf("server refused: estimated=%d bound=%d status=%d", tooLong.Estimated, tooLong.Bound, tooLong.Status)
	if tooLong.Status == 0 {
		t.Fatal("the first oversized prompt was refused locally; nothing had been learned yet")
	}
	if b.contextLimit.Bound() <= 0 {
		t.Fatal("the refusal taught the limit nothing")
	}

	// The second one must not reach the wire.
	_, err = b.complete(ctx, ovRequest{Model: model, Messages: []ovMessage{{Role: "user", Content: huge}}, MaxTokens: 4})
	if !errors.As(err, &tooLong) {
		t.Fatalf("the second oversized prompt did not produce PromptTooLongError: %v", err)
	}
	if tooLong.Status != 0 {
		t.Fatalf("the second oversized prompt reached the server (status=%d); the learned ceiling did not hold", tooLong.Status)
	}

	// A prompt inside the limit must still work, and must calibrate.
	resp, err := b.complete(ctx, ovRequest{Model: model, Messages: []ovMessage{{Role: "user", Content: "say OK"}}, MaxTokens: 4})
	if err != nil {
		t.Fatalf("a small prompt failed after learning: %v", err)
	}
	if resp.Usage.PromptTokens <= 0 {
		t.Fatal("the server reported no prompt_tokens; calibration has nothing to learn from")
	}
	if b.contextLimit.Ratio() <= 0 {
		t.Fatal("the ratio was not calibrated from a successful response")
	}
	t.Logf("calibrated ratio=%.3f (server counted %d)", b.contextLimit.Ratio(), resp.Usage.PromptTokens)
}

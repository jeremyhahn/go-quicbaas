// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package orchestration

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// scriptedAgent is a stream-json agent made of two pipes: the test reads the
// user messages the backend writes and answers each one on its own schedule.
type scriptedAgent struct {
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	in      *bufio.Scanner
}

func newScriptedAgent() *scriptedAgent {
	a := &scriptedAgent{}
	a.stdinR, a.stdinW = io.Pipe()
	a.stdoutR, a.stdoutW = io.Pipe()
	a.in = bufio.NewScanner(a.stdinR)
	return a
}

func (a *scriptedAgent) proc() *streamJSONProc {
	return &streamJSONProc{stdin: a.stdinW, stdout: a.stdoutR, gone: func() error { return errors.New("scripted agent ended") }}
}

// readPrompt blocks until the backend has written a user message and returns
// its content.
func (a *scriptedAgent) readPrompt(t *testing.T) string {
	t.Helper()
	// t.Error, not t.Fatal: these helpers run on the agent's goroutine, where
	// FailNow would stop the wrong goroutine and vet refuses it.
	if !a.in.Scan() {
		t.Error("the backend closed stdin before writing a prompt")
		return ""
	}
	var msg streamJSONUserMessage
	if err := json.Unmarshal(a.in.Bytes(), &msg); err != nil {
		t.Errorf("unmarshal the prompt: %v", err)
		return ""
	}
	return msg.Message.Content
}

func (a *scriptedAgent) emit(t *testing.T, msg streamJSONMessage) {
	t.Helper()
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Errorf("marshal a record: %v", err)
		return
	}
	if _, err := a.stdoutW.Write(append(raw, '\n')); err != nil {
		t.Errorf("write a record: %v", err)
	}
}

func (a *scriptedAgent) init(t *testing.T, sessionID string) {
	a.emit(t, streamJSONMessage{Type: "system", Subtype: "init", SessionID: sessionID, Model: "m"})
}

// reply answers the prompt most recently read: one assistant record and the
// terminal result.
func (a *scriptedAgent) reply(t *testing.T, text string) {
	a.emit(t, streamJSONMessage{Type: "assistant", Message: &streamJSONAssistant{
		Role: "assistant", Content: []streamJSONContent{{Type: "text", Text: text}}, StopReason: "end_turn", Model: "m",
	}})
	a.emit(t, streamJSONMessage{Type: "result", Subtype: "success", Result: text})
}

func (a *scriptedAgent) close() {
	_ = a.stdoutW.Close()
	_ = a.stdinR.Close()
}

// openScripted runs openStreamJSON against the scripted agent with the given
// drain bound, answering the handshake through answerHandshake on a goroutine.
func openScripted(t *testing.T, agent *scriptedAgent, drain time.Duration, answerHandshake func(prompt string)) (*ACPBackend, *streamJSONOpen) {
	t.Helper()
	b := NewACPBackend(ACPBackendConfig{Command: "claude"})
	b.streamJSONDrain = drain
	go func() {
		prompt := agent.readPrompt(t)
		agent.init(t, "sess-1")
		answerHandshake(prompt)
	}()
	open, err := b.openStreamJSON(context.Background(), agent.proc(), spawnPlan{}, "m", func() {})
	if err != nil {
		t.Fatalf("openStreamJSON: %v", err)
	}
	return b, open
}

func sessionOf(open *streamJSONOpen, agent *scriptedAgent) *acpSession {
	return &acpSession{id: open.sessionID, stdin: agent.stdinW, reader: open.reader, pending: open.pending, model: open.model}
}

// TestStreamJSON_AHandshakeStillInFlightDoesNotStealTheFirstPromptsResult is
// the burn-in defect: the handshake reply arrives AFTER the drain bound and
// after the first prompt was written. The reply owed to the handshake must
// be attributed to the handshake, and the prompt must get its own.
func TestStreamJSON_AHandshakeStillInFlightDoesNotStealTheFirstPromptsResult(t *testing.T) {
	agent := newScriptedAgent()
	defer agent.close()
	handshakeSeen := make(chan string, 1)
	b, open := openScripted(t, agent, 20*time.Millisecond, func(prompt string) {
		handshakeSeen <- prompt
		// The handshake's reply is withheld until the first real prompt has
		// been written — the retry storm the fleet measured.
	})
	if open.pending != 1 {
		t.Fatalf("pending = %d after a drain that timed out, want 1", open.pending)
	}
	sess := sessionOf(open, agent)

	type outcome struct {
		resp *PromptResponse
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		resp, err := b.promptStreamJSON(context.Background(), sess, &PromptRequest{Prompt: "do the work"})
		done <- outcome{resp, err}
	}()
	// The handshake's reply lands late; the prompt is settled behind it and
	// only then written, so the stream never carries two turns' replies for
	// one reader to attribute.
	agent.reply(t, "handshake reply")
	if got := agent.readPrompt(t); got != "do the work" {
		t.Fatalf("prompt written = %q", got)
	}
	agent.reply(t, "the work is done")

	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("prompt: %v", out.err)
		}
		if len(out.resp.Content) != 1 || out.resp.Content[0].Text != "the work is done" {
			t.Errorf("the prompt was handed the wrong reply: %+v", out.resp.Content)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the prompt never returned: its result was lost")
	}
	if sess.pending != 0 {
		t.Errorf("pending = %d after a settled prompt, want 0", sess.pending)
	}
	<-handshakeSeen
}

// TestStreamJSON_AHandshakeAnsweredInTimeLeavesNothingOwed is the ordinary
// case: the reply is consumed by the open, and the first prompt settles
// nothing.
func TestStreamJSON_AHandshakeAnsweredInTimeLeavesNothingOwed(t *testing.T) {
	agent := newScriptedAgent()
	defer agent.close()
	b, open := openScripted(t, agent, time.Second, func(string) { agent.reply(t, "hello") })
	if open.pending != 0 {
		t.Fatalf("pending = %d, want 0", open.pending)
	}
	sess := sessionOf(open, agent)
	go func() {
		agent.readPrompt(t)
		agent.reply(t, "answer")
	}()
	resp, err := b.promptStreamJSON(context.Background(), sess, &PromptRequest{Prompt: "q"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "answer" {
		t.Errorf("content = %+v", resp.Content)
	}
}

// TestStreamJSON_APromptAbandonedOnCtxIsSettledByTheNext: a caller that gives
// up on a turn leaves its result owed; the next prompt reads past it and is
// handed its own reply, not the abandoned one's.
func TestStreamJSON_APromptAbandonedOnCtxIsSettledByTheNext(t *testing.T) {
	agent := newScriptedAgent()
	defer agent.close()
	b, open := openScripted(t, agent, time.Second, func(string) { agent.reply(t, "hello") })
	sess := sessionOf(open, agent)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		agent.readPrompt(t)
		cancel() // the caller gives up before any reply
	}()
	if _, err := b.promptStreamJSON(ctx, sess, &PromptRequest{Prompt: "slow"}); err == nil {
		t.Fatal("an abandoned prompt returned a reply")
	}
	if sess.pending != 1 {
		t.Fatalf("pending = %d after an abandoned turn, want 1", sess.pending)
	}
	if sess.closed.Load() {
		t.Fatal("a caller giving up on a turn closed the session")
	}

	go func() {
		agent.reply(t, "late reply to the slow prompt")
		agent.readPrompt(t)
		agent.reply(t, "reply to the next prompt")
	}()
	resp, err := b.promptStreamJSON(context.Background(), sess, &PromptRequest{Prompt: "next"})
	if err != nil {
		t.Fatalf("next prompt: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "reply to the next prompt" {
		t.Errorf("the next prompt was handed the abandoned turn's reply: %+v", resp.Content)
	}
}

// TestStreamJSON_AFailedResultIsReportedWithItsEvidence: an is_error result
// ends the prompt with the CLI's own reason class carried on the error.
func TestStreamJSON_AFailedResultIsReportedWithItsEvidence(t *testing.T) {
	agent := newScriptedAgent()
	defer agent.close()
	b, open := openScripted(t, agent, time.Second, func(string) { agent.reply(t, "hello") })
	sess := sessionOf(open, agent)
	go func() {
		agent.readPrompt(t)
		agent.emit(t, streamJSONMessage{Type: "result", Subtype: "success", IsError: true,
			Result: "API Error: Can't reach the API server", TerminalReason: TerminalReasonAPIError, NumTurns: 1})
	}()
	_, err := b.promptStreamJSON(context.Background(), sess, &PromptRequest{Prompt: "q"})
	if err == nil {
		t.Fatal("a failed result returned a reply")
	}
	if evidence, ok := EvidenceOf(err); !ok || evidence.ReasonClass() != TerminalReasonAPIError || evidence.NumTurns != 1 {
		t.Errorf("err = %v, want the CLI's api_error and turn count carried", err)
	}
	if strings.Contains(err.Error(), "Can't reach") {
		t.Errorf("the agent's own words were rendered: %v", err)
	}
	if sess.pending != 0 {
		t.Errorf("pending = %d, want 0", sess.pending)
	}
}

// TestStreamJSON_TheStreamEndingMidTurnNamesTheAgentGone: stdout closing
// before a result is the agent going away, not a hung prompt.
func TestStreamJSON_TheStreamEndingMidTurnNamesTheAgentGone(t *testing.T) {
	agent := newScriptedAgent()
	b, open := openScripted(t, agent, time.Second, func(string) { agent.reply(t, "hello") })
	sess := sessionOf(open, agent)
	go func() {
		agent.readPrompt(t)
		agent.close()
	}()
	_, err := b.promptStreamJSON(context.Background(), sess, &PromptRequest{Prompt: "q"})
	if err == nil {
		t.Fatal("a closed stream returned a reply")
	}
	if !strings.Contains(err.Error(), "scripted agent ended") {
		t.Errorf("err = %v, want the opener's explanation of the end", err)
	}
	if !sess.closed.Load() {
		t.Error("the session was not marked closed after its stream ended")
	}
}

// TestStreamJSON_TheInitBoundIsHonoured: an agent that never says init is
// refused after the bound with an authored message.
func TestStreamJSON_TheInitBoundIsHonoured(t *testing.T) {
	agent := newScriptedAgent()
	defer agent.close()
	b := NewACPBackend(ACPBackendConfig{Command: "claude"})
	b.streamJSONInit = 30 * time.Millisecond
	go agent.readPrompt(t)
	tornDown := false
	_, err := b.openStreamJSON(context.Background(), agent.proc(), spawnPlan{}, "m", func() { tornDown = true })
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for init message") {
		t.Fatalf("err = %v, want the init timeout", err)
	}
	if !tornDown {
		t.Error("a refused open did not tear the process down")
	}
}

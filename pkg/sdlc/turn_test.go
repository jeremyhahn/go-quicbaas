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

package sdlc

import (
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestReadAgentTurn_TakesTheReplyAndTheCensus: the turn record is what the
// backend actually published — the assistant's own words, a census of every
// block, the stop reason and the token accounting.
func TestReadAgentTurn_TakesTheReplyAndTheCensus(t *testing.T) {
	turn := ReadAgentTurn(&orchestration.PromptSessionResponse{
		Content: []orchestration.ContentBlock{
			{Type: "text", Text: "reading the issue"},
			{Type: "tool_use"},
			{Type: "tool_use"},
			{Type: "text", Text: "done"},
		},
		StopReason: orchestration.StopReasonEndTurn,
		Usage:      &orchestration.TokenUsage{InputTokens: 11, OutputTokens: 7},
	})
	if turn.Reply != "reading the issue\n\ndone" {
		t.Fatalf("reply = %q", turn.Reply)
	}
	if turn.Blocks["text"] != 2 || turn.Blocks["tool_use"] != 2 {
		t.Fatalf("blocks = %v", turn.Blocks)
	}
	if turn.ToolUses() != 2 {
		t.Fatalf("tool uses = %d, want 2", turn.ToolUses())
	}
	if turn.StopReason != orchestration.StopReasonEndTurn {
		t.Fatalf("stop reason = %q", turn.StopReason)
	}
	if turn.Usage == nil || turn.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %+v", turn.Usage)
	}
	if turn.Truncated() {
		t.Fatal("a short reply was reported truncated")
	}
}

// TestReadAgentTurn_CountsToolCallsNotEveryBlock (go-quicbaas#799): a
// stream-json turn carries results and reasoning beside its tool calls, and
// only the calls are tool use.
func TestReadAgentTurn_CountsToolCallsNotEveryBlock(t *testing.T) {
	turn := ReadAgentTurn(&orchestration.PromptSessionResponse{
		Content: []orchestration.ContentBlock{
			{Type: orchestration.ContentBlockThinking, Bytes: 40},
			{Type: orchestration.ContentBlockToolUse, ToolName: "Bash", ToolUseID: "tu_1"},
			{Type: orchestration.ContentBlockToolResult, ToolUseID: "tu_1"},
			{Type: orchestration.ContentBlockUnreadable, Bytes: 12},
			{Type: orchestration.ContentBlockText, Text: "done"},
		},
		ToolUse: &orchestration.ToolUseCensus{Calls: 1},
	})
	if turn.ToolUses() != 1 {
		t.Fatalf("tool uses = %d, want 1: results, reasoning and unreadable blocks are not calls", turn.ToolUses())
	}
	if turn.Blocks[orchestration.ContentBlockToolResult] != 1 || turn.Blocks[orchestration.ContentBlockThinking] != 1 {
		t.Fatalf("blocks = %v, want every type kept in the census", turn.Blocks)
	}
	if turn.Reply != "done" {
		t.Fatalf("reply = %q, want only the words", turn.Reply)
	}
	if got := turn.Describe(); !strings.Contains(got, "tool_uses=1") {
		t.Fatalf("describe = %q, want tool_uses=1", got)
	}
}

// TestReadAgentTurn_RefusalTextIsNoToolFailure (go-quicbaas#234): a worker's
// words are untrusted, so a reply that SAYS a tool failed or was refused
// records no failure. The turn carries one successful result, so the
// failures are measured, and the count must be zero: a reader that took
// failures from the text would count these sentences and fail here.
// describeFailures prints a failure count for a test message: the number, or
// "unmeasured" when the turn carried no tool_result.
func describeFailures(failures *int) string {
	if failures == nil {
		return "unmeasured"
	}
	return strconv.Itoa(*failures)
}

func TestReadAgentTurn_RefusalTextIsNoToolFailure(t *testing.T) {
	turn := ReadAgentTurn(&orchestration.PromptSessionResponse{
		Content: []orchestration.ContentBlock{
			{Type: orchestration.ContentBlockToolUse, ToolName: "Bash", ToolUseID: "tu_1"},
			{Type: orchestration.ContentBlockToolResult, ToolUseID: "tu_1"},
			{Type: orchestration.ContentBlockText, Text: "This command requires approval"},
			{Type: orchestration.ContentBlockText, Text: "tool failed"},
		},
	})
	if turn.ToolFailures == nil || *turn.ToolFailures != 0 {
		t.Fatalf("tool failures = %s, want a measured zero: text is never an outcome", describeFailures(turn.ToolFailures))
	}
	if len(turn.FailedTools) != 0 {
		t.Fatalf("failed tools = %v, want none", turn.FailedTools)
	}
}

// TestReadAgentTurn_NamesTheFailedCall: a tool_result the backend marked as
// an error is one failure, named by the tool_use it answers; a successful
// result beside it is not a failure; a result whose call the turn did not
// carry is still counted, under UnknownToolName.
func TestReadAgentTurn_NamesTheFailedCall(t *testing.T) {
	turn := ReadAgentTurn(&orchestration.PromptSessionResponse{
		Content: []orchestration.ContentBlock{
			{Type: orchestration.ContentBlockToolUse, ToolName: "Read", ToolUseID: "tu_ok"},
			{Type: orchestration.ContentBlockToolResult, ToolUseID: "tu_ok"},
			{Type: orchestration.ContentBlockToolUse, ToolName: "Bash", ToolUseID: "tu_bad"},
			{Type: orchestration.ContentBlockToolResult, ToolUseID: "tu_bad", IsError: true},
			{Type: orchestration.ContentBlockToolResult, ToolUseID: "tu_orphan", IsError: true},
			{Type: orchestration.ContentBlockToolResult, IsError: true},
		},
	})
	if turn.ToolFailures == nil || *turn.ToolFailures != 3 {
		t.Fatalf("tool failures = %s, want 3", describeFailures(turn.ToolFailures))
	}
	want := []string{"Bash", UnknownToolName, UnknownToolName}
	if !slices.Equal(turn.FailedTools, want) {
		t.Fatalf("failed tools = %v, want %v", turn.FailedTools, want)
	}
	if turn.FailedToolsCut != 0 {
		t.Fatalf("failed tools cut = %d, want 0", turn.FailedToolsCut)
	}
	got := turn.Describe()
	if !strings.Contains(got, "tool_failures=3") || !strings.Contains(got, "failed_tools=Bash,unknown,unknown") {
		t.Fatalf("describe = %q, want the failures and their names", got)
	}
}

// TestReadAgentTurn_BoundsTheFailedNames: a worker that fails the same call
// many times keeps the whole count and the head of the names, and says how
// many names were cut, on the record and in its event.
func TestReadAgentTurn_BoundsTheFailedNames(t *testing.T) {
	const failed = MaxFailedTools + 5
	content := make([]orchestration.ContentBlock, 0, 2*failed)
	for i := 0; i < failed; i++ {
		id := "tu_" + strconv.Itoa(i)
		content = append(content,
			orchestration.ContentBlock{Type: orchestration.ContentBlockToolUse, ToolName: "Bash", ToolUseID: id},
			orchestration.ContentBlock{Type: orchestration.ContentBlockToolResult, ToolUseID: id, IsError: true},
		)
	}
	turn := ReadAgentTurn(&orchestration.PromptSessionResponse{Content: content})
	if turn.ToolFailures == nil || *turn.ToolFailures != failed {
		t.Fatalf("tool failures = %s, want %d", describeFailures(turn.ToolFailures), failed)
	}
	if len(turn.FailedTools) != MaxFailedTools || turn.FailedToolsCut != 5 {
		t.Fatalf("failed tools = %d names, %d cut; want %d and 5", len(turn.FailedTools), turn.FailedToolsCut, MaxFailedTools)
	}
	if got := turn.Describe(); !strings.Contains(got, "failed_tools_cut=5") {
		t.Fatalf("describe = %q, want failed_tools_cut=5", got)
	}
}

// TestAgentTurn_CappedBoundsFailedNamesAssembledByHand: Capped bounds what is
// stored, so a record built field by field with too many names is cut on the
// way to the store, and the capped copy does not share the original's names.
func TestAgentTurn_CappedBoundsFailedNamesAssembledByHand(t *testing.T) {
	names := make([]string, MaxFailedTools+2)
	for i := range names {
		names[i] = "Edit"
	}
	failures := len(names)
	turn := AgentTurn{ToolFailures: &failures, FailedTools: names, FailedToolsCut: 1}
	capped := turn.Capped()
	if len(capped.FailedTools) != MaxFailedTools || capped.FailedToolsCut != 3 {
		t.Fatalf("capped = %d names, %d cut; want %d and 3", len(capped.FailedTools), capped.FailedToolsCut, MaxFailedTools)
	}
	capped.FailedTools[0] = "changed"
	if turn.FailedTools[0] != "Edit" {
		t.Fatal("the capped record shares its names with the original")
	}
}

// TestReadAgentTurn_NoBlocksLeavesFailuresUnmeasured: a turn whose backend
// reported no tool_result keeps today's shape, failures absent rather than
// zero, on the record, in its JSON and in its event.
func TestReadAgentTurn_NoBlocksLeavesFailuresUnmeasured(t *testing.T) {
	for name, resp := range map[string]*orchestration.PromptSessionResponse{
		"nil response":       nil,
		"no blocks":          {},
		"only text":          {Content: []orchestration.ContentBlock{{Type: orchestration.ContentBlockText, Text: "tool failed"}}},
		"census, no results": {ToolUse: &orchestration.ToolUseCensus{Calls: 2}},
	} {
		t.Run(name, func(t *testing.T) {
			turn := ReadAgentTurn(resp)
			if turn.ToolFailures != nil || turn.FailedTools != nil || turn.FailedToolsCut != 0 {
				t.Fatalf("failures = %v %v %d, want unmeasured", turn.ToolFailures, turn.FailedTools, turn.FailedToolsCut)
			}
			encoded, err := json.Marshal(turn)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if strings.Contains(string(encoded), "tool_failures") || strings.Contains(string(encoded), "failed_tools") {
				t.Fatalf("json = %s, want no failure fields", encoded)
			}
			if strings.Contains(turn.Describe(), "tool_failures") {
				t.Fatal("an unmeasured turn described a failure count")
			}
		})
	}
}

// TestAgentTurn_SurvivesItsJSONRecord: the turn is stored and published as
// JSON, so every field must come back from its own encoding. The sentinel
// sets every exported field, checked by reflection, so a field added later
// without a value, or dropped by its tag, fails here rather than vanishing
// from the record (the contract go-quicbaas#248 keeps for crossing types).
func TestAgentTurn_SurvivesItsJSONRecord(t *testing.T) {
	failures := 2
	sentinel := AgentTurn{
		Attempt: 3, Round: 2, Elapsed: 1500 * time.Millisecond,
		Reply: "ran it\nSDLC-STOP: question", ReplyBytes: 26,
		Blocks:         map[string]int{"text": 1, "tool_use": 2, "tool_result": 2},
		ToolFailures:   &failures,
		FailedTools:    []string{"Bash", UnknownToolName},
		FailedToolsCut: 4,
		StopReason:     orchestration.StopReasonEndTurn,
		Usage:          &orchestration.TokenUsage{InputTokens: 10, OutputTokens: 20},
		StopCode:       TurnStopQuestion,
		StopDeclared:   true,
		Outcome:        TurnOutcomeQuestion,
		PromptDigest:   "sha256:abc",
	}
	value := reflect.ValueOf(sentinel)
	for i := 0; i < value.NumField(); i++ {
		if value.Type().Field(i).IsExported() && value.Field(i).IsZero() {
			t.Errorf("AgentTurn.%s is left at its zero value by the sentinel: give it one", value.Type().Field(i).Name)
		}
	}
	encoded, err := json.Marshal(sentinel)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded AgentTurn
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(sentinel, decoded) {
		t.Fatalf("round trip lost a field:\n sent: %#v\n  got: %#v", sentinel, decoded)
	}
}

// TestReadAgentTurn_CountsTheBackendToolCensus: a backend that carries only
// text in Content reports its tool calls on the census, and the turn counts
// them — a turn that used tools is not read as one that used none (#799).
func TestReadAgentTurn_CountsTheBackendToolCensus(t *testing.T) {
	turn := ReadAgentTurn(&orchestration.PromptSessionResponse{
		Content:    []orchestration.ContentBlock{{Type: "text", Text: "done"}},
		StopReason: orchestration.StopReasonEndTurn,
		ToolUse:    &orchestration.ToolUseCensus{Calls: 3},
	})
	if turn.ToolUses() != 3 {
		t.Fatalf("tool uses = %d, want the census's 3", turn.ToolUses())
	}
	if turn.Blocks["text"] != 1 {
		t.Fatalf("blocks = %v, want the text block still counted", turn.Blocks)
	}
}

// TestReadAgentTurn_KeepsAReportedZeroApartFromNoReport: a zero census is
// recorded as a zero tool_use entry and described as 0, an absent census as
// no entry at all and described as unreported, the way a nil Usage and a zero
// one differ.
func TestReadAgentTurn_KeepsAReportedZeroApartFromNoReport(t *testing.T) {
	reported := ReadAgentTurn(&orchestration.PromptSessionResponse{
		Content: []orchestration.ContentBlock{{Type: "text", Text: "done"}},
		ToolUse: &orchestration.ToolUseCensus{},
	})
	if calls, ok := reported.Blocks["tool_use"]; !ok || calls != 0 {
		t.Fatalf("blocks = %v, want a zero tool_use entry for a reported zero", reported.Blocks)
	}
	if got := reported.DescribeToolUses(); got != "0" {
		t.Fatalf("reported zero described as %q, want 0", got)
	}
	unreported := ReadAgentTurn(&orchestration.PromptSessionResponse{
		Content: []orchestration.ContentBlock{{Type: "text", Text: "done"}},
	})
	if _, ok := unreported.Blocks["tool_use"]; ok {
		t.Fatalf("blocks = %v, want no tool_use entry when the backend reports none", unreported.Blocks)
	}
	if !strings.Contains(unreported.Describe(), "tool_uses=unreported") {
		t.Fatalf("describe = %q, want the missing measurement named", unreported.Describe())
	}
}

// TestReadAgentTurn_AnUnreadableToolItemIsUnreportedNotANonAnswer: a codex
// turn holding an item the backend could not decode arrives with no census.
// It must be described as unreported and must not be taken for a fast,
// tool-less non-answer, however short and undeclared its reply.
func TestReadAgentTurn_AnUnreadableToolItemIsUnreportedNotANonAnswer(t *testing.T) {
	turn := ReadAgentTurn(&orchestration.PromptSessionResponse{
		Content: []orchestration.ContentBlock{
			{Type: orchestration.ContentBlockUnreadable, Bytes: 64},
			{Type: orchestration.ContentBlockText, Text: "The expired monitor was watching the sweep logs."},
		},
		StopReason: orchestration.StopReasonEndTurn,
	})
	turn.Elapsed = 451 * time.Millisecond
	if !strings.Contains(turn.Describe(), "tool_uses=unreported") {
		t.Fatalf("describe = %q, want tool_uses=unreported", turn.Describe())
	}
	if turn.NonAnswer() {
		t.Fatalf("NonAnswer() = true for a turn whose tool use is unknown: %+v", turn)
	}
}

// TestReadAgentTurn_AnAnswerWithNothingInIt: a nil response, an empty content
// list and blank text are the SAME turn — a worker that said nothing — and
// none of them is an error here.
func TestReadAgentTurn_AnAnswerWithNothingInIt(t *testing.T) {
	for name, resp := range map[string]*orchestration.PromptSessionResponse{
		"nil response":       nil,
		"no content":         {},
		"blank text":         {Content: []orchestration.ContentBlock{{Type: "text", Text: "   \n\t"}}},
		"untyped tool block": {Content: []orchestration.ContentBlock{{Type: "  "}}},
	} {
		t.Run(name, func(t *testing.T) {
			turn := ReadAgentTurn(resp)
			if turn.Reply != "" {
				t.Fatalf("reply = %q, want empty", turn.Reply)
			}
			if turn.ReplyBytes != 0 {
				t.Fatalf("reply bytes = %d", turn.ReplyBytes)
			}
			if ClassifyTurn(turn).Outcome != TurnOutcomeSilent {
				t.Fatalf("outcome = %s, want silent", ClassifyTurn(turn).Outcome)
			}
		})
	}
}

// TestReadAgentTurn_KeepsTheWholeReplyAndCapsOnlyWhatIsWritten is the
// defect go-quicbaas#787's own mechanism nearly reintroduced: a worker that
// explains itself at length and ends, correctly, with a stop declaration
// past the cap.
//
// Capping at read time cut the declaration off before anything classified
// it, so the turn read as a worker CLAIMING work it had not done — charged,
// re-prompted, budget burned, on exactly the workers with most to say. The
// whole reply is kept and classified; only what is written down is bounded.
func TestReadAgentTurn_KeepsTheWholeReplyAndCapsOnlyWhatIsWritten(t *testing.T) {
	long := strings.Repeat("the workspace has two candidate stores. ", 200)
	if len(long) <= TurnReplyLimit {
		t.Fatalf("the fixture must exceed the cap, got %d bytes", len(long))
	}
	turn := ReadAgentTurn(&orchestration.PromptSessionResponse{
		Content: []orchestration.ContentBlock{{Type: "text", Text: long + "\nSDLC-STOP: question"}},
	})

	// Classified from the whole reply, so the declaration past the cap is read.
	if verdict := ClassifyTurn(turn); verdict.Outcome != TurnOutcomeQuestion {
		t.Fatalf("outcome = %s, want question; the declaration past the cap was lost",
			verdict.Outcome)
	}
	if !turn.StopDeclared || turn.StopCode != TurnStopQuestion {
		t.Fatalf("declaration = %q/%v", turn.StopCode, turn.StopDeclared)
	}
	if len(turn.Reply) <= TurnReplyLimit {
		t.Fatal("the run's own copy of the reply was capped")
	}

	// What is WRITTEN is bounded, and still says what the turn was.
	stored := turn.Capped()
	if len(stored.Reply) > TurnReplyLimit+len(turnReplyEllipsis) {
		t.Fatalf("the stored reply is %d bytes", len(stored.Reply))
	}
	if !strings.HasSuffix(stored.Reply, turnReplyEllipsis) || !stored.Truncated() {
		t.Fatal("the stored reply does not say it was cut")
	}
	if stored.ReplyBytes != turn.ReplyBytes {
		t.Fatalf("the stored record lost the original length: %d", stored.ReplyBytes)
	}
	if ClassifyTurn(stored).Outcome != TurnOutcomeQuestion {
		t.Fatal("the stored record no longer classifies; the declaration fields did not survive")
	}
	if !strings.Contains(turn.Describe(), "truncated=true") {
		t.Fatal("the event does not say it carried a cut reply")
	}
	if len(turn.Describe()) > TurnReplyLimit+len(turnReplyEllipsis)+512 {
		t.Fatal("the event carried the whole uncapped reply")
	}
}

// TestCapReply_CutsOnARuneBoundary: the cap never splits a rune, because
// the result is read by a person and rendered by whatever shows it.
func TestCapReply_CutsOnARuneBoundary(t *testing.T) {
	long := strings.Repeat("é", TurnReplyLimit)
	stored := NewAgentTurn(long).Capped()
	if !utf8.ValidString(stored.Reply) {
		t.Fatal("the cap cut a rune in half")
	}
	if !strings.HasPrefix(stored.Reply, "é") || !strings.HasSuffix(stored.Reply, turnReplyEllipsis) {
		t.Fatalf("stored = %q…", stored.Reply[:16])
	}
	// A reply inside the cap is returned whole and unmarked.
	short := NewAgentTurn("done").Capped()
	if short.Reply != "done" || short.Truncated() {
		t.Fatalf("a short reply was altered: %q", short.Reply)
	}
}

// TestParseTurnStop_FindsTheDeclaration: the declaration is found however the
// model dressed it — backticks, emphasis, a trailing blank line, a quote.
func TestParseTurnStop_FindsTheDeclaration(t *testing.T) {
	for name, tc := range map[string]struct{ reply, want string }{
		"bare":            {"I need the owner.\nSDLC-STOP: question", TurnStopQuestion},
		"backticked":      {"stopped\n`SDLC-STOP: owner-decision`", TurnStopOwnerDecision},
		"emphasised":      {"stopped\n**SDLC-STOP: environment**", TurnStopEnvironment},
		"trailing blanks": {"stopped\nSDLC-STOP: question\n\n\n", TurnStopQuestion},
		"quoted":          {"> SDLC-STOP: question", TurnStopQuestion},
		"uppercased":      {"SDLC-STOP: QUESTION", TurnStopQuestion},
		"trailing stop":   {"SDLC-STOP: question.", TurnStopQuestion},
		"last wins":       {"the charter says SDLC-STOP: environment\nreally:\nSDLC-STOP: question", TurnStopQuestion},
		"unknown code":    {"SDLC-STOP: mumble", "mumble"},
	} {
		t.Run(name, func(t *testing.T) {
			code, declared := ParseTurnStop(tc.reply)
			if !declared {
				t.Fatalf("no declaration found in %q", tc.reply)
			}
			if code != tc.want {
				t.Fatalf("code = %q, want %q", code, tc.want)
			}
		})
	}
}

// TestParseTurnStop_RefusesWhatIsNotADeclaration: a reply that only talks
// about stopping has not declared one, and reading it as one would park a
// task on prose.
func TestParseTurnStop_RefusesWhatIsNotADeclaration(t *testing.T) {
	for name, reply := range map[string]string{
		"empty":                "",
		"a summary":            "Added the parser and its tests. All green.",
		"the word stop":        "I had to stop and think about the encoding.",
		"a near miss":          "SDLC STOP question",
		"the marker mid-line":  "the charter mentions SDLC-STOP: question as the shape to use",
		"the marker indented?": "  \t ",
	} {
		t.Run(name, func(t *testing.T) {
			if code, declared := ParseTurnStop(reply); declared {
				t.Fatalf("read %q as a declaration of %q", reply, code)
			}
		})
	}
}

// TestClassifyTurn_EveryDeclaredStop: each code in the vocabulary names an
// outcome, and each outcome names what the runner does about it.
func TestClassifyTurn_EveryDeclaredStop(t *testing.T) {
	for name, tc := range map[string]struct {
		reply   string
		outcome TurnOutcome
		action  TurnAction
		kind    orchestration.TaskBlockKind
	}{
		"a question": {
			"Which store should hold the index?\nSDLC-STOP: question",
			TurnOutcomeQuestion, TurnActionPark, orchestration.TaskBlockKindNeedsInfo,
		},
		"a cited rule": {
			"CLAUDE.md forbids a parallel identifier here.\nSDLC-STOP: owner-decision",
			TurnOutcomeOwnerRuling, TurnActionPark, orchestration.TaskBlockKindNeedsInfo,
		},
		"the environment": {
			"the module cache is unreachable\nSDLC-STOP: environment",
			TurnOutcomeEnvironment, TurnActionPark, orchestration.TaskBlockKindBlocked,
		},
		"a code nobody knows": {
			"stopped\nSDLC-STOP: vibes",
			TurnOutcomeUnreadable, TurnActionPark, orchestration.TaskBlockKindBlocked,
		},
		"work that never happened": {
			"Implemented the parser and added tests.",
			TurnOutcomeClaimedWork, TurnActionCharge, "",
		},
		"nothing at all": {
			"",
			TurnOutcomeSilent, TurnActionRetry, "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			verdict := ClassifyTurn(NewAgentTurn(tc.reply))
			if verdict.Outcome != tc.outcome {
				t.Fatalf("outcome = %s, want %s", verdict.Outcome, tc.outcome)
			}
			if verdict.Action != tc.action {
				t.Fatalf("action = %s, want %s", verdict.Action, tc.action)
			}
			if verdict.BlockKind != tc.kind {
				t.Fatalf("block kind = %q, want %q", verdict.BlockKind, tc.kind)
			}
			if verdict.Headline == "" {
				t.Fatal("a verdict with no headline says nothing to the person reading it")
			}
		})
	}
}

// TestClassifyTurn_TheTableIsComplete: every outcome the classifier can
// produce has a verdict, every declared code names an outcome that has one,
// and every parking verdict names a kind the task store will accept. A row
// missing from either map is an outcome nothing could act on.
func TestClassifyTurn_TheTableIsComplete(t *testing.T) {
	outcomes := []TurnOutcome{
		TurnOutcomeSilent, TurnOutcomeQuestion, TurnOutcomeOwnerRuling,
		TurnOutcomeEnvironment, TurnOutcomeUnreadable, TurnOutcomeClaimedWork,
		// TurnOutcomeSatisfied is the one row that neither parks nor charges:
		// the work was already done, so the task concludes. The assertions
		// below cover it without special-casing — it names no BlockKind and no
		// Condition precisely because nobody is waiting on anything.
		TurnOutcomeSatisfied,
		// TurnOutcomeNonAnswer neither parks nor charges either: nothing was
		// judged, so the same prompt is simply sent again.
		TurnOutcomeNonAnswer,
	}
	for _, outcome := range outcomes {
		verdict, ok := turnVerdicts[outcome]
		if !ok {
			t.Fatalf("outcome %s has no verdict", outcome)
		}
		if verdict.Outcome != outcome {
			t.Fatalf("verdict for %s reports itself as %s", outcome, verdict.Outcome)
		}
		if _, known := turnActions[verdict.Action]; !known {
			t.Fatalf("outcome %s names action %s, which the runner does not perform", outcome, verdict.Action)
		}
		if verdict.Action == TurnActionPark && verdict.BlockKind.Validate() != nil {
			t.Fatalf("outcome %s parks with kind %q, which the task store refuses", outcome, verdict.BlockKind)
		}
		if verdict.Action != TurnActionPark && verdict.BlockKind != "" {
			t.Fatalf("outcome %s does not park but names kind %q", outcome, verdict.BlockKind)
		}
		if verdict.Action == TurnActionPark {
			condition := UnblockCondition{Kind: verdict.Condition}
			if err := condition.Validate(); err != nil {
				t.Fatalf("outcome %s parks on condition %q, which is not in the vocabulary: %v",
					outcome, verdict.Condition, err)
			}
			// A worker's stop is settled by a person by construction; a
			// machine-checkable condition here would be the factory clearing
			// the question it is forbidden to answer.
			if verdict.Condition.MachineCheckable() {
				t.Fatalf("outcome %s parks on %q, which a sweep would clear by itself",
					outcome, verdict.Condition)
			}
		}
		if verdict.Action != TurnActionPark && verdict.Condition != "" {
			t.Fatalf("outcome %s does not park but names condition %q", outcome, verdict.Condition)
		}
	}
	if len(turnVerdicts) != len(outcomes) {
		t.Fatalf("the verdict table holds %d rows for %d outcomes", len(turnVerdicts), len(outcomes))
	}
	for code, outcome := range stopCodeOutcomes {
		if _, ok := turnVerdicts[outcome]; !ok {
			t.Fatalf("code %q names outcome %s, which has no verdict", code, outcome)
		}
		if !strings.Contains(WorkerCharter(sdlcPrompts), code) {
			t.Fatalf("code %q is in the vocabulary but the charter never teaches it", code)
		}
	}
	if !strings.Contains(WorkerCharter(sdlcPrompts), TurnStopMarker) {
		t.Fatal("the charter does not teach the stop marker the parser reads")
	}
	if silentEscalation.BlockKind.Validate() != nil || silentEscalation.Action != TurnActionPark {
		t.Fatalf("the silent escalation is not a park: %+v", silentEscalation)
	}
	if (UnblockCondition{Kind: silentEscalation.Condition}).Validate() != nil {
		t.Fatalf("the silent escalation parks on condition %q", silentEscalation.Condition)
	}
}

// nonAnswerTurn is the turn go-quicbaas#62 recorded, parameterised: a
// persistent session answering its own queued notification instead of the
// round prompt — text, no declaration, no tool use, back in 451ms.
func nonAnswerTurn(reply string, elapsed time.Duration, tools int) AgentTurn {
	turn := NewAgentTurn(reply)
	turn.Elapsed = elapsed
	turn.StopReason = orchestration.StopReasonEndTurn
	turn.Blocks = map[string]int{
		orchestration.ContentBlockText:    1,
		orchestration.ContentBlockToolUse: tools,
	}
	return turn
}

// TestAgentTurn_NonAnswerReadsStructureNotProse: each condition of the rule,
// asserted by removing exactly one of them from the turn that satisfies all.
func TestAgentTurn_NonAnswerReadsStructureNotProse(t *testing.T) {
	const missed = "The expired monitor was watching the first-round sweep logs."
	for name, tc := range map[string]struct {
		turn AgentTurn
		want bool
	}{
		"the measured case": {nonAnswerTurn(missed, 451*time.Millisecond, 0), true},
		"at the bound": {
			nonAnswerTurn(missed, NonAnswerTurnBound, 0), false,
		},
		"a turn that worked": {
			nonAnswerTurn(missed, NonAnswerTurnBound+time.Second, 0), false,
		},
		"a turn that used a tool": {
			nonAnswerTurn(missed, 451*time.Millisecond, 3), false,
		},
		"a declared stop": {
			nonAnswerTurn("already done\nSDLC-STOP: satisfied", 451*time.Millisecond, 0), false,
		},
		"no text at all": {
			nonAnswerTurn("", 451*time.Millisecond, 0), false,
		},
		"an unmeasured turn": {
			nonAnswerTurn(missed, 0, 0), false,
		},
		"a turn whose tool use was not reported": {
			func() AgentTurn {
				turn := nonAnswerTurn(missed, 451*time.Millisecond, 0)
				delete(turn.Blocks, orchestration.ContentBlockToolUse)
				return turn
			}(), false,
		},
		"a turn the backend cut off": {
			func() AgentTurn {
				turn := nonAnswerTurn(missed, 451*time.Millisecond, 0)
				turn.StopReason = "max_tokens"
				return turn
			}(), false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.turn.NonAnswer(); got != tc.want {
				t.Fatalf("NonAnswer() = %v, want %v for %+v", got, tc.want, tc.turn)
			}
		})
	}
}

// TestClassifyTurn_ANonAnswerIsRepromptedAndEverythingElseIsNot: the
// classifier half. The fast, tool-less, undeclared reply is the one turn
// between `silent` and `claimed_work`, and neither of its neighbours moves.
func TestClassifyTurn_ANonAnswerIsRepromptedAndEverythingElseIsNot(t *testing.T) {
	const missed = "The expired monitor was watching the first-round sweep logs."
	for name, tc := range map[string]struct {
		turn    AgentTurn
		outcome TurnOutcome
		action  TurnAction
	}{
		"answered something else": {
			nonAnswerTurn(missed, 451*time.Millisecond, 0), TurnOutcomeNonAnswer, TurnActionReprompt,
		},
		"claimed work it did not do": {
			nonAnswerTurn("Implemented the parser and added tests.", 8*time.Minute, 0),
			TurnOutcomeClaimedWork, TurnActionCharge,
		},
		"stopped fast on a question": {
			nonAnswerTurn("Which store?\nSDLC-STOP: question", 900*time.Millisecond, 0),
			TurnOutcomeQuestion, TurnActionPark,
		},
		"said nothing": {
			nonAnswerTurn("", 451*time.Millisecond, 0), TurnOutcomeSilent, TurnActionRetry,
		},
	} {
		t.Run(name, func(t *testing.T) {
			verdict := ClassifyTurn(tc.turn)
			if verdict.Outcome != tc.outcome {
				t.Fatalf("outcome = %s, want %s", verdict.Outcome, tc.outcome)
			}
			if verdict.Action != tc.action {
				t.Fatalf("action = %s, want %s", verdict.Action, tc.action)
			}
		})
	}
}

// TestAgentTurn_DescribeCarriesTheCountsAndTheReply: the job event is the
// only account of a turn, so it holds both what it cost and what was said.
func TestAgentTurn_DescribeCarriesTheCountsAndTheReply(t *testing.T) {
	turn := AgentTurn{
		Attempt: 2, Round: 1, Elapsed: 3 * time.Minute,
		Reply: "asked about the store\nSDLC-STOP: question", ReplyBytes: 41,
		Blocks:     map[string]int{"text": 1, "tool_use": 4},
		StopReason: orchestration.StopReasonEndTurn,
		Usage:      &orchestration.TokenUsage{InputTokens: 900, OutputTokens: 12},
		Outcome:    TurnOutcomeQuestion,
	}
	got := turn.Describe()
	for _, want := range []string{
		"attempt=2", "round=1", "elapsed=3m0s", "reply_bytes=41", "tool_uses=4",
		"blocks=text:1,tool_use:4", "stop_reason=end_turn", "tokens_in=900", "tokens_out=12",
		"outcome=question", "SDLC-STOP: question",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("describe = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "truncated=true") {
		t.Fatal("an untruncated turn claimed truncation")
	}
}

// TestAgentTurn_DescribeSaysSoWhenThereIsNothingToSay: a silent turn produces
// an event that states the silence, because an event with an empty tail is
// indistinguishable from an event that lost its payload.
func TestAgentTurn_DescribeSaysSoWhenThereIsNothingToSay(t *testing.T) {
	got := AgentTurn{Attempt: 1, Elapsed: time.Second}.Describe()
	if !strings.Contains(got, "the worker answered with no text") {
		t.Fatalf("describe = %q", got)
	}
	if !strings.Contains(got, "tool_uses=unreported") || !strings.Contains(got, "reply_bytes=0") {
		t.Fatalf("describe = %q", got)
	}
	if strings.Contains(got, "blocks=") {
		t.Fatalf("an absent census was rendered as one: %q", got)
	}
	truncated := AgentTurn{Reply: "head", ReplyBytes: 5000}
	if !truncated.Truncated() || !strings.Contains(truncated.Describe(), "truncated=true") {
		t.Fatalf("describe = %q", truncated.Describe())
	}
}

// TestTurnParkReason_QuotesTheWorkerUnderATypedCondition: the reason a person
// reads is the verdict's sentence and the worker's own words, and the reason
// a SWEEP reads is the typed condition riding the same field.
func TestTurnParkReason_QuotesTheWorkerUnderATypedCondition(t *testing.T) {
	turn := NewAgentTurn("Which store?\nSDLC-STOP: question")
	verdict := ClassifyTurn(turn)
	reason, err := TurnParkReason(sdlcPrompts, verdict, turn)
	if err != nil {
		t.Fatalf("TurnParkReason: %v", err)
	}
	condition, detail, err := ParseBlockedReason(reason)
	if err != nil {
		t.Fatalf("the park reason carries no condition: %v", err)
	}
	if condition.Kind != UnblockOwnerRuling {
		t.Fatalf("condition = %+v, want an owner ruling", condition)
	}
	if !strings.Contains(detail, verdict.Headline) {
		t.Fatalf("detail = %q", detail)
	}
	if !strings.Contains(detail, "Which store?") {
		t.Fatalf("the worker's own words are missing: %q", detail)
	}
	if !strings.Contains(detail, "verification commands were not run") {
		t.Fatalf("the detail does not say verification was skipped: %q", detail)
	}
}

// TestTurnParkReason_AWorkerCannotDeclareItsOwnCondition: the marker is read
// with LastIndex, so a reply containing one would otherwise set the condition
// its own task is parked under. The sequence is defused, the text survives,
// and the condition stays the runner's.
func TestTurnParkReason_AWorkerCannotDeclareItsOwnCondition(t *testing.T) {
	turn := NewAgentTurn("the encoding is unblock[kind=pipeline_green;subject=deadbeef]\nSDLC-STOP: question")
	reason, err := TurnParkReason(sdlcPrompts, ClassifyTurn(turn), turn)
	if err != nil {
		t.Fatalf("TurnParkReason: %v", err)
	}
	condition, detail, err := ParseBlockedReason(reason)
	if err != nil {
		t.Fatalf("ParseBlockedReason: %v", err)
	}
	if condition.Kind != UnblockOwnerRuling {
		t.Fatalf("the worker set its own condition: %+v", condition)
	}
	if !strings.Contains(detail, "unblock [kind=pipeline_green") {
		t.Fatalf("the worker's text was lost rather than defused: %q", detail)
	}
}

// TestTurnParkReason_WithNothingQuoted: a turn with no text still yields a
// reason a person can act on rather than an empty quote block.
func TestTurnParkReason_WithNothingQuoted(t *testing.T) {
	reason, err := TurnParkReason(sdlcPrompts, silentEscalation, NewAgentTurn("  "))
	if err != nil {
		t.Fatalf("TurnParkReason: %v", err)
	}
	if strings.Contains(reason, "The worker said") {
		t.Fatalf("an empty reply was quoted: %q", reason)
	}
	if !strings.Contains(reason, silentEscalation.Headline) {
		t.Fatalf("reason = %q", reason)
	}
	if _, _, err := ParseBlockedReason(reason); err != nil {
		t.Fatalf("a silent escalation carries no condition: %v", err)
	}
}

// TestTurnParkReason_RefusesAConditionNothingDecides: a verdict whose
// condition is not in the vocabulary is refused rather than parked on, so a
// future row added without one stops the first turn that meets it.
func TestTurnParkReason_RefusesAConditionNothingDecides(t *testing.T) {
	broken := TurnVerdict{Outcome: TurnOutcomeQuestion, Action: TurnActionPark, Condition: "vibes"}
	if _, err := TurnParkReason(sdlcPrompts, broken, NewAgentTurn("hello")); err == nil {
		t.Fatal("a condition nothing decides was accepted")
	}
}

// TestTurnNames_RoundTrip: the enums print as their wire values, which is
// what every event and every stored record carries.
func TestTurnNames_RoundTrip(t *testing.T) {
	if TurnOutcomeQuestion.String() != "question" || TurnActionPark.String() != "park" {
		t.Fatalf("names = %q / %q", TurnOutcomeQuestion, TurnActionPark)
	}
	if TurnOutcome("").String() != "" || TurnAction("").String() != "" {
		t.Fatal("an empty enum printed as something")
	}
}

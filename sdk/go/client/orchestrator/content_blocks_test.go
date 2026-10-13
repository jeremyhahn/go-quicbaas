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

package orchestrator

import (
	"encoding/json"
	"reflect"
	"testing"
)

// toolTurnContent is a tool call and its failed result as the REST, QUIC and
// Unix transports put them on the wire.
const toolTurnContent = `"content":[` +
	`{"type":"tool_use","text":"go test","tool_name":"Bash","tool_use_id":"toolu_01","bytes":42},` +
	`{"type":"tool_result","tool_use_id":"toolu_01","is_error":true,"bytes":7}]`

func TestInferenceResponseJSON_KeepsToolBlockMetadata(t *testing.T) {
	var resp InferenceResponse
	if err := json.Unmarshal([]byte(`{`+toolTurnContent+`,"model":"m"}`), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := []InferenceContentBlock{
		{Type: "tool_use", Text: "go test", ToolName: "Bash", ToolUseID: "toolu_01", Bytes: 42},
		{Type: "tool_result", ToolUseID: "toolu_01", IsError: true, Bytes: 7},
	}
	if !reflect.DeepEqual(resp.Content, want) {
		t.Errorf("Content = %+v, want %+v", resp.Content, want)
	}
}

func TestPromptSessionWireJSON_KeepsToolBlockMetadata(t *testing.T) {
	var wire PromptSessionWire
	if err := json.Unmarshal([]byte(`{"session_id":"s1",`+toolTurnContent+`}`), &wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := []PromptContentBlock{
		{Type: "tool_use", Text: "go test", ToolName: "Bash", ToolUseID: "toolu_01", Bytes: 42},
		{Type: "tool_result", ToolUseID: "toolu_01", IsError: true, Bytes: 7},
	}
	if !reflect.DeepEqual(wire.Content, want) {
		t.Errorf("Content = %+v, want %+v", wire.Content, want)
	}
}

func TestPromptSessionWireJSON_WrongBlockShapeIsAnError(t *testing.T) {
	var wire PromptSessionWire
	if err := json.Unmarshal([]byte(`{"content":[{"bytes":"many"}]}`), &wire); err == nil {
		t.Fatal("Unmarshal error = nil, want a type error for a string byte count")
	}
}

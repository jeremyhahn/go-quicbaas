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

package uicontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/workerimage"
)

// =============================================================================
// The contract: a TypeScript response type may only name fields the server sends
// =============================================================================
//
// A field the browser reads that the server does not send is not an error
// anywhere. `encoding/json` omits it, `fetch` returns an object without it,
// TypeScript has been told it exists, and the component renders `undefined`
// as an em-dash — which reads as "the server said there is nothing here". The
// only detector was a person noticing a column was wrong.
//
// So each binding below pins one TypeScript interface to the Go struct that
// serialises into it, and the test asserts that every field the interface
// declares is a field the struct writes. A rename is allowed, but it has to
// be DECLARED and it has to be PERFORMED: a transport must carry out the
// rename, and the test checks the mapping exists rather than taking the
// declaration's word for it. That is the half that was missing — the Go
// client and the Wails binding renamed `state`→`status` and
// `last_activity`→`last_active`, the REST client renamed neither, and the
// TypeScript type asserted the renamed names for all three transports.

// binding pairs one TypeScript interface with the Go struct behind it.
type binding struct {
	// tsInterface is the interface's name.
	tsInterface string

	// tsFile is the sdk/typescript/src file that declares it; empty is
	// types.ts.
	tsFile string

	// goStruct is a zero value of the struct the server serialises.
	goStruct any

	// goStructName names it for failure messages.
	goStructName string

	// renames maps a TypeScript field to the json tag it stands for. Every
	// entry must be carried out by a transport mapping, which the test
	// verifies against mappingFiles.
	renames map[string]string

	// mappingFiles are the transport sources that must perform the renames.
	mappingFiles []string

	// absent lists TypeScript fields the server provably does not send, each
	// with the reason. AN ENTRY HERE IS A DEBT, NOT A PASS: the test asserts
	// the field really is missing from the Go struct, so the moment the
	// server starts sending it the entry must be deleted or this fails.
	absent map[string]string
}

func bindings() []binding {
	return []binding{
		{
			// The session list: what every session surface in the browser is
			// actually driven by (sessionStore reads listSessions).
			tsInterface:  "SessionStatusResponse",
			goStruct:     orchestration.SessionInfo{},
			goStructName: "orchestration.SessionInfo",
			renames: map[string]string{
				"status":      "state",
				"last_active": "last_activity",
			},
			mappingFiles: []string{
				filepath.Join("sdk", "typescript", "src", "rest.ts"),
			},
		},
		{
			tsInterface:  "SessionInfo",
			goStruct:     orchestration.SessionInfo{},
			goStructName: "orchestration.SessionInfo",
		},
		{
			tsInterface:  "Task",
			goStruct:     orchestration.Task{},
			goStructName: "orchestration.Task",
		},
		{
			tsInterface:  "JobStatus",
			goStruct:     orchestration.JobStatus{},
			goStructName: "orchestration.JobStatus",
		},
		{
			tsInterface:  "FleetNode",
			goStruct:     orchestration.FleetNode{},
			goStructName: "orchestration.FleetNode",
		},
		{
			tsInterface:  "SpawnSessionResponse",
			goStruct:     orchestration.SpawnSessionResponse{},
			goStructName: "orchestration.SpawnSessionResponse",
		},
		{
			tsInterface:  "JobConfig",
			goStruct:     orchestration.JobConfig{},
			goStructName: "orchestration.JobConfig",
		},
		{
			tsInterface:  "WorkerImagePromotion",
			goStruct:     workerimage.Promotion{},
			goStructName: "workerimage.Promotion",
		},
		// The tenant agent catalog.
		{tsInterface: "Agent", tsFile: "agents.ts", goStruct: orchestration.Agent{}, goStructName: "orchestration.Agent"},
		{tsInterface: "AgentPool", tsFile: "agents.ts", goStruct: orchestration.AgentPool{}, goStructName: "orchestration.AgentPool"},
		{tsInterface: "AgentRef", tsFile: "agents.ts", goStruct: orchestration.AgentRef{}, goStructName: "orchestration.AgentRef"},
		{tsInterface: "AgentUsage", tsFile: "agents.ts", goStruct: orchestration.AgentUsage{}, goStructName: "orchestration.AgentUsage"},
		{tsInterface: "AgentAttempt", tsFile: "agents.ts", goStruct: orchestration.AgentAttempt{}, goStructName: "orchestration.AgentAttempt"},
		{tsInterface: "ProviderEndpoint", tsFile: "agents.ts", goStruct: orchestration.ProviderEndpoint{}, goStructName: "orchestration.ProviderEndpoint"},
		{tsInterface: "AgentAccountPolicy", tsFile: "agents.ts", goStruct: orchestration.AgentAccountPolicy{}, goStructName: "orchestration.AgentAccountPolicy"},
		{tsInterface: "AgentPrompt", tsFile: "agents.ts", goStruct: orchestration.AgentPrompt{}, goStructName: "orchestration.AgentPrompt"},
		{tsInterface: "AgentPlacement", tsFile: "agents.ts", goStruct: orchestration.RolePolicy{}, goStructName: "orchestration.RolePolicy"},
		// The binding's references into it.
		{tsInterface: "ProjectBinding", tsFile: "sdlc.ts", goStruct: sdlc.ProjectBinding{}, goStructName: "sdlc.ProjectBinding"},
		{tsInterface: "ReviewerConfig", tsFile: "sdlc.ts", goStruct: sdlc.ReviewerConfig{}, goStructName: "sdlc.ReviewerConfig"},
		{tsInterface: "ReviewerSelection", tsFile: "sdlc.ts", goStruct: sdlc.ReviewerSelection{}, goStructName: "sdlc.ReviewerSelection"},
	}
}

// source reads the TypeScript file that declares b's interface.
func (b binding) source(t *testing.T, root string) (string, string) {
	t.Helper()
	file := b.tsFile
	if file == "" {
		file = "types.ts"
	}
	rel := filepath.Join("sdk", "typescript", "src", file)
	return rel, readFile(t, filepath.Join(root, rel))
}

func TestTypeScriptResponseTypesNameOnlyFieldsTheServerSends(t *testing.T) {
	root := repoRoot(t)

	for _, b := range bindings() {
		t.Run(b.tsInterface, func(t *testing.T) {
			_, src := b.source(t, root)
			tsFields := typeScriptInterfaceFields(t, src, b.tsInterface)
			if len(tsFields) == 0 {
				t.Fatalf("interface %s declares no fields; the parse found nothing, which is a defect in this test rather than a passing contract", b.tsInterface)
			}
			wire := jsonFieldsOf(t, b.goStruct)

			for _, field := range tsFields {
				switch {
				case b.absent[field] != "":
					// Checked BEFORE the plain case, or a debt the server has
					// since paid would match there and never be reported —
					// which is how job_id stayed listed as absent after
					// SessionInfo started sending it (go-quicbaas#860).
					if wire[field] {
						t.Errorf("%s.%s is listed as absent from the wire (%s), but %s does send it — delete the exception",
							b.tsInterface, field, b.absent[field], b.goStructName)
					}
				case wire[field]:
					// The plain case: the names agree.
				case b.renames[field] != "":
					target := b.renames[field]
					if !wire[target] {
						t.Errorf("%s.%s is declared as a rename of %s.%s, but that struct has no json tag %q — the rename points at nothing",
							b.tsInterface, field, b.goStructName, target, target)
					}
				default:
					t.Errorf("%s.%s has no counterpart on %s. The browser reads this field and the server never writes it, so it renders as an em-dash and nothing reports a failure. Either the name is wrong, or declare a rename a transport performs, or record it in `absent` with the issue that will add it. The wire fields are: %s",
						b.tsInterface, field, b.goStructName, strings.Join(sortedKeys(wire), ", "))
				}
			}
		})
	}
}

// TestDeclaredRenamesArePerformed checks the other half. A rename the type
// asserts and no transport carries out is precisely the go-quicbaas#855
// defect: `SessionStatusResponse.last_active` was declared, the Go client and
// the Wails binding renamed the field, and the REST client returned the
// server's body untouched — so the same component worked on the desktop and
// showed an em-dash in a browser tab.
func TestDeclaredRenamesArePerformed(t *testing.T) {
	root := repoRoot(t)

	for _, b := range bindings() {
		if len(b.renames) == 0 {
			continue
		}
		t.Run(b.tsInterface, func(t *testing.T) {
			if len(b.mappingFiles) == 0 {
				t.Fatalf("%s declares renames but names no transport that performs them", b.tsInterface)
			}
			for _, rel := range b.mappingFiles {
				src := readFile(t, filepath.Join(root, rel))
				for tsField, wireField := range b.renames {
					if !strings.Contains(src, tsField) || !strings.Contains(src, wireField) {
						t.Errorf("%s does not perform the %s rename %s ← %s: the file mentions %q=%v and %q=%v. A type that renames a field without a mapping behind it is a field the caller never receives",
							rel, b.tsInterface, tsField, wireField,
							tsField, strings.Contains(src, tsField),
							wireField, strings.Contains(src, wireField))
					}
				}
			}
		})
	}
}

// TestBindingsNameRealTypes guards the test itself. A binding whose interface
// has been renamed would otherwise silently check nothing, and a contract test
// that checks nothing is worse than none: it reports success.
func TestBindingsNameRealTypes(t *testing.T) {
	root := repoRoot(t)

	for _, b := range bindings() {
		rel, src := b.source(t, root)
		if !strings.Contains(src, "export interface "+b.tsInterface+" {") {
			t.Errorf("%s declares no interface %s; the binding checks nothing", rel, b.tsInterface)
		}
		if len(jsonFieldsOf(t, b.goStruct)) == 0 {
			t.Errorf("%s has no json-tagged fields; the binding checks nothing", b.goStructName)
		}
	}
}

// TestJSONFieldsOfReadsWhatAMarshallerWrites: a field a type's MarshalJSON
// adds is on the wire even though no tag names it, and a tagged field is
// still counted when its zero value is omitted. Without the first half the
// contract refuses a TypeScript field the server does send; without the
// second it forgets every omitempty field.
func TestJSONFieldsOfReadsWhatAMarshallerWrites(t *testing.T) {
	fields := jsonFieldsOf(t, orchestration.Task{})
	for _, derived := range []string{"charged_attempts", "effective_budget", "fault_class", "fault_origin"} {
		if !fields[derived] {
			t.Errorf("the derived key %q that Task.MarshalJSON writes is not counted as a wire field", derived)
		}
	}
	for _, omitted := range []string{"assigned_to", "retry_not_before"} {
		if !fields[omitted] {
			t.Errorf("the omitempty key %q is lost when the zero value is encoded", omitted)
		}
	}
	if fields["taskRecord"] {
		t.Error("the unexported embedded record leaked onto the wire as a field of its own")
	}
}

// =============================================================================
// Reading the two sides
// =============================================================================

// jsonFieldsOf is every name the struct writes onto the wire, by reflection
// rather than by parsing — this side is exact. An embedded struct contributes
// its own fields, as encoding/json does.
//
// A TYPE WITH ITS OWN MarshalJSON WRITES WHAT THAT METHOD WRITES, which its
// tags cannot say: orchestration.Task adds the accounting it derives on read
// (go-quicbaas#947). So such a type is also ENCODED, and the keys its zero
// value produces join the reflected tags. The union is needed because an
// omitempty tag is absent from a zero value's encoding, and the encoding is
// needed because a derived key has no tag at all.
func jsonFieldsOf(t *testing.T, v any) map[string]bool {
	t.Helper()
	fields := map[string]bool{}
	collectJSONFields(reflect.TypeOf(v), fields)
	if _, custom := v.(json.Marshaler); custom {
		for name := range encodedKeys(t, v) {
			fields[name] = true
		}
	}
	return fields
}

// encodedKeys is the set of top-level keys v's JSON encoding carries.
func encodedKeys(t *testing.T, v any) map[string]bool {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode %T: %v", v, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("%T does not encode as a JSON object: %v", v, err)
	}
	keys := make(map[string]bool, len(object))
	for name := range object {
		keys[name] = true
	}
	return keys
}

func collectJSONFields(typ reflect.Type, into map[string]bool) {
	if typ == nil {
		return
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		switch {
		case name == "-":
			continue
		case field.Anonymous && name == "":
			// Embedded and unnamed: its fields are promoted onto the wire.
			collectJSONFields(field.Type, into)
		case name != "":
			into[name] = true
		case field.IsExported():
			// No tag: encoding/json uses the Go field name verbatim.
			into[field.Name] = true
		}
	}
}

// tsFieldPattern matches a property line of a flat TypeScript interface:
// two-space indent, a name, an optional `?`, a colon. Comment and blank lines
// do not match, and neither does a nested object's inner line, which is
// indented further.
var tsFieldPattern = regexp.MustCompile(`^  ([A-Za-z_][A-Za-z0-9_]*)\??:`)

// typeScriptInterfaceFields lists the properties an interface declares, in
// source order. The parse is deliberately strict: a body it cannot read makes
// the test fail rather than pass with an empty field list.
func typeScriptInterfaceFields(t *testing.T, src, name string) []string {
	t.Helper()
	header := "export interface " + name + " {"
	start := strings.Index(src, header)
	if start == -1 {
		t.Fatalf("sdk/typescript/src/types.ts has no %q", header)
	}
	body := src[start+len(header):]
	// A flat interface ends at the first closing brace in column 0.
	end := strings.Index(body, "\n}")
	if end == -1 {
		t.Fatalf("interface %s is not terminated by a closing brace in column 0", name)
	}
	body = body[:end]

	if strings.Contains(body, "\n    ") {
		t.Fatalf("interface %s has a nested body; this parse only reads flat interfaces and would silently miss fields", name)
	}

	var fields []string
	for _, line := range strings.Split(body, "\n") {
		if match := tsFieldPattern.FindStringSubmatch(line); match != nil {
			fields = append(fields, match[1])
		}
	}
	return fields
}

// =============================================================================
// Locating the sources
// =============================================================================

// repoRoot walks up from the test's directory to the module root, so the test
// reads the same sources the build does no matter where it is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %s", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory; the repository root could not be found")
		}
		dir = parent
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a path built from the module root
	if err != nil {
		t.Fatalf("read %s: %s", path, err)
	}
	return string(data)
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

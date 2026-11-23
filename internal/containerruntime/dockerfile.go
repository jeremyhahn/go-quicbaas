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

package containerruntime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The in-process image build reads Dockerfiles itself rather than handing
// them to a docker or podman binary. It reads the subset a build with an
// empty context can use: FROM, ARG, ENV, RUN, COPY (from a heredoc or an
// earlier stage), USER, WORKDIR, LABEL, ENTRYPOINT, CMD and SHELL, with
// line continuations, comments, heredocs and JSON forms. Anything else is
// an error naming the line, never silently skipped.

// dockerfileInstruction is one instruction as written, before any variable
// is expanded.
type dockerfileInstruction struct {
	// Command is the instruction keyword, upper-cased.
	Command string

	// Flags are the leading --name=value options, in order.
	Flags []dockerfileFlag

	// Args is the rest of the instruction: one raw string in shell form,
	// or the list in JSON form.
	Args []string

	// JSON reports whether Args came from a JSON array.
	JSON bool

	// Heredocs are the here-documents the instruction opened, in order.
	Heredocs []dockerfileHeredoc

	// Line is the 1-based line the instruction starts on.
	Line int
}

// dockerfileFlag is one --name=value instruction option.
type dockerfileFlag struct {
	Name  string
	Value string
}

// dockerfileHeredoc is one here-document body.
type dockerfileHeredoc struct {
	// Name is the delimiter word.
	Name string

	// Body is the text between the instruction and the delimiter line,
	// each line ending in a newline.
	Body string

	// Expand reports whether the delimiter was unquoted, so the body's
	// variables are expanded.
	Expand bool
}

// flag returns the value of the named option and whether it was given.
func (i *dockerfileInstruction) flag(name string) (string, bool) {
	for _, f := range i.Flags {
		if f.Name == name {
			return f.Value, true
		}
	}
	return "", false
}

// DockerfileError is a Dockerfile the in-process build cannot read.
type DockerfileError struct {
	Line    int
	Message string
}

func (e *DockerfileError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("dockerfile line %d: %s", e.Line, e.Message)
	}
	return "dockerfile: " + e.Message
}

// dockerfileCommands are the instructions the in-process build runs.
var dockerfileCommands = map[string]bool{
	"FROM": true, "ARG": true, "ENV": true, "RUN": true, "COPY": true,
	"USER": true, "WORKDIR": true, "LABEL": true, "ENTRYPOINT": true,
	"CMD": true, "SHELL": true,
}

// heredocPattern matches a heredoc opener: <<NAME, <<-NAME, <<'NAME' or
// <<"NAME".
var heredocPattern = regexp.MustCompile(`<<(-?)(['"]?)([A-Za-z_][A-Za-z0-9_]*)(['"]?)`)

// parseDockerfile splits src into instructions.
func parseDockerfile(src []byte) ([]dockerfileInstruction, error) {
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	if err := checkParserDirectives(lines); err != nil {
		return nil, err
	}

	var out []dockerfileInstruction
	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}

		start := i
		var logical strings.Builder
		for i < len(lines) {
			line := lines[i]
			i++
			body := strings.TrimSpace(line)
			if logical.Len() > 0 && (body == "" || strings.HasPrefix(body, "#")) {
				// Blank and comment lines inside a continuation are dropped.
				continue
			}
			text := strings.TrimRight(line, " \t")
			if cut, ok := strings.CutSuffix(text, `\`); ok {
				logical.WriteString(cut)
				continue
			}
			logical.WriteString(text)
			break
		}

		instruction, err := parseInstruction(strings.TrimSpace(logical.String()), start+1)
		if err != nil {
			return nil, err
		}
		if instruction.Command == "RUN" || instruction.Command == "COPY" {
			if i, err = readHeredocs(&instruction, lines, i); err != nil {
				return nil, err
			}
		}
		out = append(out, instruction)
	}
	if len(out) == 0 {
		return nil, &DockerfileError{Message: "no instructions"}
	}
	return out, nil
}

// checkParserDirectives refuses an escape character other than the
// backslash. A syntax directive names a frontend image and is ignored.
func checkParserDirectives(lines []string) error {
	for n, line := range lines {
		directive, ok := strings.CutPrefix(strings.TrimSpace(line), "#")
		if !ok {
			return nil
		}
		key, value, ok := strings.Cut(directive, "=")
		if !ok {
			return nil
		}
		if strings.EqualFold(strings.TrimSpace(key), "escape") && strings.TrimSpace(value) != `\` {
			return &DockerfileError{Line: n + 1, Message: "only the backslash escape character is supported"}
		}
	}
	return nil
}

// parseInstruction splits one logical line into its keyword, options and
// arguments.
func parseInstruction(text string, line int) (dockerfileInstruction, error) {
	keyword, rest, _ := strings.Cut(text, " ")
	if tab := strings.IndexByte(keyword, '\t'); tab >= 0 {
		keyword, rest = keyword[:tab], keyword[tab+1:]+" "+rest
	}
	instruction := dockerfileInstruction{Command: strings.ToUpper(keyword), Line: line}
	if !dockerfileCommands[instruction.Command] {
		return instruction, &DockerfileError{Line: line, Message: instruction.Command + " is not supported by the in-process build"}
	}

	rest = strings.TrimSpace(rest)
	for strings.HasPrefix(rest, "--") {
		word, after, _ := strings.Cut(rest, " ")
		name, value, ok := strings.Cut(word[2:], "=")
		if !ok || name == "" {
			return instruction, &DockerfileError{Line: line, Message: "option " + word + " needs a value"}
		}
		instruction.Flags = append(instruction.Flags, dockerfileFlag{Name: name, Value: value})
		rest = strings.TrimSpace(after)
	}

	if strings.HasPrefix(rest, "[") {
		var list []string
		if err := json.Unmarshal([]byte(rest), &list); err == nil {
			instruction.Args, instruction.JSON = list, true
			return instruction, nil
		}
	}
	if rest == "" {
		return instruction, &DockerfileError{Line: line, Message: instruction.Command + " needs arguments"}
	}
	instruction.Args = []string{rest}
	return instruction, nil
}

// readHeredocs reads the bodies of the heredocs instruction opens from
// lines[next:], and returns the index of the first line after them.
func readHeredocs(instruction *dockerfileInstruction, lines []string, next int) (int, error) {
	if instruction.JSON {
		return next, nil
	}
	for _, match := range heredocPattern.FindAllStringSubmatch(instruction.Args[0], -1) {
		stripTabs, open, name, closing := match[1] == "-", match[2], match[3], match[4]
		if open != closing {
			return next, &DockerfileError{Line: instruction.Line, Message: "heredoc " + name + " has unbalanced quotes"}
		}
		var body strings.Builder
		closed := false
		for next < len(lines) {
			line := lines[next]
			next++
			if stripTabs {
				line = strings.TrimLeft(line, "\t")
			}
			if line == name {
				closed = true
				break
			}
			body.WriteString(line)
			body.WriteByte('\n')
		}
		if !closed {
			return next, &DockerfileError{Line: instruction.Line, Message: "heredoc " + name + " is never closed"}
		}
		instruction.Heredocs = append(instruction.Heredocs, dockerfileHeredoc{
			Name: name, Body: body.String(), Expand: open == "",
		})
	}
	return next, nil
}

// expandWords expands the variables in s the way a Dockerfile does and
// splits the result into words at unquoted whitespace. Quotes and
// backslashes are removed; single quotes stop expansion. A variable that
// is not set expands to nothing.
func expandWords(s string, lookup func(string) (string, bool)) ([]string, error) {
	w := wordExpander{src: []rune(s), lookup: lookup, split: true}
	return w.run()
}

// expandWord is expandWords without the split: the whole of s is one word.
func expandWord(s string, lookup func(string) (string, bool)) (string, error) {
	w := wordExpander{src: []rune(s), lookup: lookup}
	words, err := w.run()
	if err != nil || len(words) == 0 {
		return "", err
	}
	return words[0], nil
}

// expandHeredoc expands the variables in an unquoted heredoc body. Quotes
// are kept, as a shell keeps them in a here-document.
func expandHeredoc(s string, lookup func(string) (string, bool)) (string, error) {
	w := wordExpander{src: []rune(s), lookup: lookup, keepQuotes: true}
	words, err := w.run()
	if err != nil || len(words) == 0 {
		return "", err
	}
	return words[0], nil
}

type wordExpander struct {
	src        []rune
	pos        int
	lookup     func(string) (string, bool)
	split      bool
	keepQuotes bool
}

func (w *wordExpander) run() ([]string, error) {
	var words []string
	var current strings.Builder
	inWord := false
	var quote rune
	for w.pos < len(w.src) {
		r := w.src[w.pos]
		switch {
		case quote == 0 && w.split && (r == ' ' || r == '\t' || r == '\n'):
			if inWord {
				words = append(words, current.String())
				current.Reset()
				inWord = false
			}
			w.pos++
			continue
		case !w.keepQuotes && (r == '\'' || r == '"') && (quote == 0 || quote == r):
			if quote == 0 {
				quote = r
			} else {
				quote = 0
			}
			inWord = true
			w.pos++
			continue
		case r == '\\' && quote != '\'' && w.pos+1 < len(w.src):
			next := w.src[w.pos+1]
			if quote == '"' && next != '"' && next != '\\' && next != '$' || w.keepQuotes && next != '$' && next != '\\' {
				current.WriteRune(r)
			}
			current.WriteRune(next)
			w.pos += 2
		case r == '$' && quote != '\'':
			value, err := w.variable()
			if err != nil {
				return nil, err
			}
			current.WriteString(value)
		default:
			current.WriteRune(r)
			w.pos++
		}
		inWord = true
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote in %q", quote, string(w.src))
	}
	if inWord || !w.split {
		words = append(words, current.String())
	}
	return words, nil
}

// variable expands the $NAME or ${NAME...} at w.pos.
func (w *wordExpander) variable() (string, error) {
	w.pos++ // the $
	if w.pos >= len(w.src) {
		return "$", nil
	}
	if w.src[w.pos] != '{' {
		name := w.name()
		if name == "" {
			return "$", nil
		}
		value, _ := w.lookup(name)
		return value, nil
	}

	w.pos++ // the {
	name := w.name()
	if name == "" {
		return "", fmt.Errorf("bad substitution in %q", string(w.src))
	}
	value, set := w.lookup(name)
	if w.pos < len(w.src) && w.src[w.pos] == '}' {
		w.pos++
		return value, nil
	}

	colon := false
	if w.pos < len(w.src) && w.src[w.pos] == ':' {
		colon = true
		w.pos++
	}
	if w.pos >= len(w.src) {
		return "", fmt.Errorf("bad substitution in %q", string(w.src))
	}
	op := w.src[w.pos]
	w.pos++
	word, err := w.until('}')
	if err != nil {
		return "", err
	}
	usable := set && (!colon || value != "")
	switch op {
	case '-':
		if usable {
			return value, nil
		}
		return word, nil
	case '+':
		if usable {
			return word, nil
		}
		return "", nil
	case '?':
		if usable {
			return value, nil
		}
		return "", fmt.Errorf("%s: %s", name, word)
	}
	return "", fmt.Errorf("unsupported substitution ${%s%c...} in %q", name, op, string(w.src))
}

// name reads a variable name at w.pos.
func (w *wordExpander) name() string {
	start := w.pos
	for w.pos < len(w.src) {
		r := w.src[w.pos]
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || w.pos > start && r >= '0' && r <= '9' {
			w.pos++
			continue
		}
		break
	}
	return string(w.src[start:w.pos])
}

// until expands the text up to the matching closing rune, which it
// consumes, honouring nested ${...}.
func (w *wordExpander) until(closing rune) (string, error) {
	var out strings.Builder
	for w.pos < len(w.src) {
		r := w.src[w.pos]
		switch {
		case r == closing:
			w.pos++
			return out.String(), nil
		case r == '\\' && w.pos+1 < len(w.src):
			out.WriteRune(w.src[w.pos+1])
			w.pos += 2
		case r == '$':
			value, err := w.variable()
			if err != nil {
				return "", err
			}
			out.WriteString(value)
		default:
			out.WriteRune(r)
			w.pos++
		}
	}
	return "", fmt.Errorf("missing %c in %q", closing, string(w.src))
}

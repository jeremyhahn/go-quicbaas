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

package prompts

import (
	"errors"
	"io/fs"
	"sort"
	"strings"
)

// FileExtension is the suffix of a prompt file. A prompt is markdown because
// markdown is what the agents reading it are shown everywhere else.
const FileExtension = ".md"

// ErrDefaultsUnreadable indicates embedded prompt markdown that could not be
// read.
var ErrDefaultsUnreadable = errors.New("prompts: embedded defaults are unreadable")

// DefaultsUnreadableError reports embedded markdown a set could not load.
type DefaultsUnreadableError struct {
	// Dir is the embedded directory that was read.
	Dir string

	// Cause is what the file system said.
	Cause error
}

// Error implements the error interface.
func (e *DefaultsUnreadableError) Error() string {
	message := ErrDefaultsUnreadable.Error() + ": " + e.Dir
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *DefaultsUnreadableError) Unwrap() error { return ErrDefaultsUnreadable }

// Defaults reads every markdown file directly under dir, keyed by base name
// without its extension. It is how an owning package turns its embedded
// system-tenant markdown into the Default of each PromptSpec.
//
// A FILE'S LAST NEWLINE IS NOT PART OF THE PROMPT. Every text file ends with
// one; a prompt that carried it would make "did my editor add a trailing
// newline" a question about what an agent is told. Exactly one is removed, so
// a body that really must end on a blank line ends its file with two.
func Defaults(fsys fs.FS, dir string) (map[string]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, &DefaultsUnreadableError{Dir: dir, Cause: err}
	}
	bodies := make(map[string]string, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, FileExtension) {
			continue
		}
		raw, readErr := fs.ReadFile(fsys, dir+"/"+name)
		if readErr != nil {
			return nil, &DefaultsUnreadableError{Dir: dir + "/" + name, Cause: readErr}
		}
		bodies[strings.TrimSuffix(name, FileExtension)] = trimFinalNewline(string(raw))
	}
	if len(bodies) == 0 {
		return nil, &DefaultsUnreadableError{Dir: dir, Cause: fs.ErrNotExist}
	}
	return bodies, nil
}

// MustDefaults reads embedded defaults or panics. It is called from package
// initialization, where markdown the binary was built without is a binary
// that cannot prompt anybody.
func MustDefaults(fsys fs.FS, dir string) map[string]string {
	bodies, err := Defaults(fsys, dir)
	if err != nil {
		panic(err)
	}
	return bodies
}

// UnusedDefaults names every body in bodies that spec does not declare.
//
// A markdown file no specification reads is a prompt a person can edit and
// nothing will ever send — the most expensive kind of dead code, because it
// looks like it works. Owning packages assert on this.
func UnusedDefaults(spec SetSpec, bodies map[string]string) []string {
	unused := make([]string, 0)
	for name := range bodies {
		if _, declared := spec.Prompt(name); !declared {
			unused = append(unused, name)
		}
	}
	sort.Strings(unused)
	return unused
}

// trimFinalNewline removes exactly one trailing newline, and the carriage
// return before it, when there is one.
func trimFinalNewline(content string) string {
	if !strings.HasSuffix(content, "\n") {
		return content
	}
	content = content[:len(content)-1]
	return strings.TrimSuffix(content, "\r")
}

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

package main

import (
	"io"
	"os"

	"github.com/spf13/cobra"
)

// PromptAppendReadError indicates the --prompt-append-file source could not
// be read.
type PromptAppendReadError struct {
	Source string
	Err    error
}

// Error implements the error interface.
func (e *PromptAppendReadError) Error() string {
	return "failed to read the prompt to append from " + e.Source + ": " + e.Err.Error()
}

// Unwrap exposes the underlying I/O error.
func (e *PromptAppendReadError) Unwrap() error { return e.Err }

// promptAppendFlags are the --prompt-append and --prompt-append-file flags a
// command that runs an Agent carries: run-time prompt material appended after
// the Agent's resolved prompt, given inline or read from a file ('-' reads
// stdin). The server bounds its size.
type promptAppendFlags struct {
	text string
	file string
}

// register adds both flags to cmd, mutually exclusive. appliesTo says what
// the material is appended to, for the help text.
func (f *promptAppendFlags) register(cmd *cobra.Command, appliesTo string) {
	cmd.Flags().StringVar(&f.text, "prompt-append", "",
		"Prompt material appended after "+appliesTo+" (at most 64 KiB)")
	cmd.Flags().StringVar(&f.file, "prompt-append-file", "",
		"Read the prompt material to append from a file; '-' reads stdin")
	cmd.MarkFlagsMutuallyExclusive("prompt-append", "prompt-append-file")
}

// value returns the material to append: the inline text, else the file's
// contents, else nothing.
func (f *promptAppendFlags) value(stdin io.Reader) (string, error) {
	switch f.file {
	case "":
		return f.text, nil
	case "-":
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", &PromptAppendReadError{Source: "stdin", Err: err}
		}
		return string(data), nil
	default:
		data, err := os.ReadFile(f.file)
		if err != nil {
			return "", &PromptAppendReadError{Source: f.file, Err: err}
		}
		return string(data), nil
	}
}

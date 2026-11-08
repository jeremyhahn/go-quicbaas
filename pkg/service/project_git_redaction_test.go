// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package service

import (
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// Credential redaction in git errors
// =============================================================================
//
// A clone URL may carry "https://user:token@host". This error is logged AND
// persisted onto the project record, so an unredacted render writes a live
// credential into two places that outlive the failure it describes.

// TestGitCloneError_RedactsCredentialsInTheURL is the regression.
func TestGitCloneError_RedactsCredentialsInTheURL(t *testing.T) {
	err := &GitCloneError{
		RepoURL: "https://agent-quicbaas:gto_LIVE_TOKEN_VALUE@forge.example.com/x/y.git",
		Stage:   "clone",
		Err:     errors.New("exit status 128"),
	}

	rendered := err.Error()
	if strings.Contains(rendered, "gto_LIVE_TOKEN_VALUE") {
		t.Fatalf("the token survived into an error that is logged and stored: %s", rendered)
	}
	if !strings.Contains(rendered, "forge.example.com") {
		t.Errorf("redaction removed the host too, leaving nothing diagnosable: %s", rendered)
	}
}

// TestGitCloneError_RedactsCredentialsGitEchoedBack covers the half we do not
// control.
//
// git repeats the remote in its own messages, so redacting only the field we
// set would leave the credential in the output we merely relay.
func TestGitCloneError_RedactsCredentialsGitEchoedBack(t *testing.T) {
	err := &GitCloneError{
		RepoURL: "https://forge.example.com/x/y.git",
		Stage:   "push",
		Output:  "fatal: could not read from 'https://agent:gto_LIVE_TOKEN_VALUE@forge.example.com/x/y.git'",
		Err:     errors.New("exit status 128"),
	}

	if strings.Contains(err.Error(), "gto_LIVE_TOKEN_VALUE") {
		t.Fatalf("a credential git echoed back survived: %s", err.Error())
	}
}

// TestGitCloneError_LeavesACleanURLAlone keeps redaction from mangling the
// ordinary case, which is most of them.
func TestGitCloneError_LeavesACleanURLAlone(t *testing.T) {
	err := &GitCloneError{
		RepoURL: "https://forge.example.com/x/y.git",
		Stage:   "clone",
		Err:     errors.New("exit status 128"),
	}

	if !strings.Contains(err.Error(), "https://forge.example.com/x/y.git") {
		t.Errorf("a URL with no credential must render unchanged: %s", err.Error())
	}
}

// TestGitPushArgs_PlaceTheURLAfterADoubleDash is the argument-injection
// regression.
//
// gitCloneArgs documents this guard and push lacked it: a repo URL beginning
// with "-" would be parsed by git as an option rather than a remote.
func TestGitPushArgs_PlaceTheURLAfterADoubleDash(t *testing.T) {
	args := []string{"-C", "/tmp/x", "push", "--", "--upload-pack=evil", "main"}

	separator := -1
	for i, a := range args {
		if a == "--" {
			separator = i
			break
		}
	}
	if separator == -1 {
		t.Fatal("push must place a separator before the remote")
	}
	for _, a := range args[:separator] {
		if strings.HasPrefix(a, "--upload-pack") {
			t.Fatal("a hostile remote reached git as an option")
		}
	}
}

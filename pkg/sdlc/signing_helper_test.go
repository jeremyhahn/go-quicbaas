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
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/gitsign"
)

const testHelperProgram = "/usr/local/bin/quicbaas-git-sign"

// helperSession is a signing session that signs through the MCP helper. Its
// socket is still set: choosing the helper does not take the socket away.
func helperSession() *SigningSession {
	return &SigningSession{
		SocketPath: "/run/quicbaas/signing/agent.sock",
		PublicKey:  testSigningKey,
		Helper: &SigningHelper{
			Program: testHelperProgram,
			Env: map[string]string{
				gitsign.EnvMCPURL:           "https://quicbaas.internal:8443/mcp",
				gitsign.EnvKeyName:          "project-signing",
				orchestration.MCPCertEnvVar: "/run/quicbaas/mcp/session.crt",
				orchestration.MCPKeyEnvVar:  "/run/quicbaas/mcp/session.key",
				orchestration.MCPCAEnvVar:   "/run/quicbaas/mcp/ca.crt",
			},
		},
	}
}

func TestGitConfigArgs_AHelperSessionSignsThroughTheHelper(t *testing.T) {
	config := strings.Join(gitConfigArgs(testOrchProject(), helperSession()), " ")
	for _, want := range []string{
		"gpg.format=ssh",
		"user.signingkey=key::" + testSigningKey,
		"commit.gpgsign=true",
		"gpg.ssh.program=" + testHelperProgram,
	} {
		if !strings.Contains(config, want) {
			t.Errorf("config %q is missing %q", config, want)
		}
	}
}

func TestGitConfigArgs_ASocketSessionNamesNoProgram(t *testing.T) {
	for name, signing := range map[string]*SigningSession{
		"socket session": {SocketPath: "/run/agent.sock", PublicKey: testSigningKey},
		"no session":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			config := strings.Join(gitConfigArgs(testOrchProject(), signing), " ")
			if strings.Contains(config, "gpg.ssh.program") {
				t.Fatalf("config %q names a signing program", config)
			}
		})
	}
}

func TestSigningEnv_OnlyAHelperSessionCarriesEnvironment(t *testing.T) {
	if env := signingEnv(helperSession()); !maps.Equal(env, helperSession().Helper.Env) {
		t.Fatalf("signingEnv = %v, want the helper's", env)
	}
	for name, signing := range map[string]*SigningSession{
		"socket session": {SocketPath: "/run/agent.sock", PublicKey: testSigningKey},
		"no session":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			if env := signingEnv(signing); env != nil {
				t.Fatalf("signingEnv = %v, want none", env)
			}
		})
	}
}

// TestPublisher_AHelperSessionCommitsThroughTheHelper: the commit names the
// helper and carries its environment; no other command does, so the session's
// MCP settings reach only the git command that signs.
func TestPublisher_AHelperSessionCommitsThroughTheHelper(t *testing.T) {
	container := &fakeContainer{changed: true}
	signing := helperSession()
	pub := newTestPublisher(container, signing)

	if _, err := pub.Publish(testPublishPlan()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if !slices.Contains(container.commitConfig, "gpg.ssh.program="+testHelperProgram) {
		t.Fatalf("commit config %q does not name the helper", container.commitConfig)
	}
	commits := 0
	for _, exec := range container.execs {
		_, args := splitGitConfig(exec.Cmd[1:])
		if len(args) > 0 && args[0] == "commit" {
			commits++
			if !maps.Equal(exec.Env, signing.Helper.Env) {
				t.Fatalf("commit env = %v, want the helper's", exec.Env)
			}
			continue
		}
		if exec.Env[gitsign.EnvMCPURL] != "" {
			t.Fatalf("%v carried the helper's environment", args)
		}
	}
	if commits != 1 {
		t.Fatalf("ran %d commits, want 1", commits)
	}
}

func TestTrainBuild_AHelperSessionMergesThroughTheHelper(t *testing.T) {
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{
		"rev-parse FETCH_HEAD": {Stdout: "abc123\n"},
		"rev-parse HEAD":       {Stdout: "merged1\n"},
	}}
	builder := newScriptedBuilder(git)
	builder.signing = helperSession()

	if commit, ejection, err := builder.Add(buildCar); err != nil || ejection != nil {
		t.Fatalf("Add = %q %+v %v", commit, ejection, err)
	}
	var merge *orchestration.ExecSpec
	for _, spec := range git.specs {
		if _, args := splitGitConfig(spec.Cmd[1:]); len(args) > 0 && args[0] == "merge" {
			merge = spec
		}
	}
	if merge == nil {
		t.Fatalf("no merge ran: %v", git.ran)
	}
	config, _ := splitGitConfig(merge.Cmd[1:])
	if !slices.Contains(config, "gpg.ssh.program="+testHelperProgram) {
		t.Fatalf("merge config %v does not name the helper", config)
	}
	if !maps.Equal(merge.Env, builder.signing.Helper.Env) {
		t.Fatalf("merge env = %v, want the helper's", merge.Env)
	}
}

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

// =============================================================================
// Built-in role charters
// =============================================================================
//
// AN AGENT'S PROMPT IS OPTIONAL. A spawn by Agent resolves its system prompt
// in this order:
//
//  1. the prompt the spawn itself names (an SDLC runner's role charter, a
//     workflow role's system_prompt);
//  2. the Agent's own prompt;
//  3. the built-in charter of the role the spawn is for, where one exists;
//  4. no system prompt, leaving the runtime's own default in force.
//
// The charters live with the runners that own them — the SDLC package owns
// the worker's, the reviewer's and triage's — and that package imports this
// one, so it registers them here at init rather than being imported.

// RoleCharterFunc renders a role's built-in charter.
type RoleCharterFunc func() string

// roleCharters maps a role to its built-in charter. It is written only by
// RegisterRoleCharter from package init functions, which the Go runtime runs
// one at a time before main, and only read afterwards.
var roleCharters = map[AgentRole]RoleCharterFunc{}

// RegisterRoleCharter installs the built-in charter an Agent with no prompt
// of its own starts with when it is spawned for role. It is called from a
// package init function; a second registration for one role replaces the
// first, and a nil charter removes it.
func RegisterRoleCharter(role AgentRole, charter RoleCharterFunc) {
	if charter == nil {
		delete(roleCharters, role)
		return
	}
	roleCharters[role] = charter
}

// RoleCharter returns the built-in charter for role, and false when the role
// has none.
func RoleCharter(role AgentRole) (string, bool) {
	charter, ok := roleCharters[role]
	if !ok {
		return "", false
	}
	return charter(), true
}

// resolvePrompt returns the system prompt a spawn of this Agent for role
// starts with when the spawn names none: the Agent's own prompt, else the
// role's built-in charter, else nothing.
func (a Agent) resolvePrompt(role AgentRole) (string, error) {
	prompt, err := a.Prompt.Render()
	if err != nil || prompt != "" {
		return prompt, err
	}
	charter, _ := RoleCharter(role)
	return charter, nil
}

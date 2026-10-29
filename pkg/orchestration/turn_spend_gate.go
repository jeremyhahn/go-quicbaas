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
	"context"
	"time"
)

// =============================================================================
// The per-turn spend gate — the cap is asked again before every turn.
//
// The spawn gate (accountResolver.authorizeSpend) and the remote release gate
// (AccountService.MaterializeAccountCredential) ask the spend authority ONCE,
// when the credential is handed out. A session then runs as many turns as it
// likes against that credential, so one that outlives its account's window —
// an hour-long worker crossing a five-hour cap part way through — is never
// asked again (go-quicbaas#782).
//
// This gate asks the SAME authority before each turn, from the account the
// live session record names now (rotation moves it), and refuses the turn
// before the backend runs it. A refused turn is therefore never charged, and
// the refusal is the spawn gate's own typed error, so it classifies as
// account_quota_exhausted exactly as a refused spawn does.
//
// Every turn is driven from here, including a turn on a session a remote
// daemon hosts: the control plane prompts it through runPromptTurn, so the
// envelope that daemon cached at materialization stops being usable for the
// next turn the moment the account is over its cap.
//
// THE COST IS ONE ACCOUNT READ AND ONE BOUNDED LEDGER SCAN PER TURN, and no
// cached verdict sits in front of it: a cache would be a second, staler
// authority, which is the disagreement asking the one authority avoids. A turn
// is minutes of agent work; the scan is not what it waits on.
// =============================================================================

// SetSpendAuthority wires the spend cap every pooled session's turn is checked
// against. It is the authority the spawn gate asks, so the two cannot disagree
// about whether an account is over its cap. Leaving it unset gates a session
// only at spawn.
func (s *agentServiceImpl) SetSpendAuthority(spend SpendAuthority) {
	s.turnSpend = spend
}

// authorizeTurnSpend asks the spend authority whether the pooled account
// serving sessionID may run one more turn.
//
// A session with no pooled account, or a service with no authority or account
// store wired, is admitted: there is no cap to check. An account record that
// cannot be read refuses the turn with the read error — a cap that cannot be
// checked is not a cap that was met, which is the same fail-closed rule the
// authority applies to a ledger it cannot read.
//
// The estimate is zero, as at spawn: nothing measures what a turn will cost
// before it runs, so the honest question is whether the account is ALREADY
// over.
func (s *agentServiceImpl) authorizeTurnSpend(ctx context.Context, sessionID string) error {
	if s.turnSpend == nil || s.accountStore == nil || sessionID == "" {
		return nil
	}
	mem, ok := s.liveSessionRecord(sessionID)
	if !ok {
		return nil
	}
	accountID := mem.AccountID()
	if accountID == "" {
		return nil
	}
	account, err := s.accountStore.Get(ctx, mem.TenantID, accountID)
	if err != nil {
		return err
	}
	return s.turnSpend.Authorize(ctx, SpendAuthorizeRequest{Account: account}, time.Now().UTC())
}

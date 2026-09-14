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
	"errors"
	"net/http"
)

// =============================================================================
// When a forge cleanup will never succeed
// =============================================================================
//
// AN OPERATOR MUST ALWAYS BE ABLE TO REMOVE A LOCAL RECORD. Unbind offboards
// the forge before deleting the binding, so any offboard failure used to strand
// the binding — and a stranded binding is not inert: the triage backstop starts
// a job for every BOUND project, so it fails a leased job on every pass, for
// ever, with no verb able to stop it.
//
// BUT "IGNORE ALL OFFBOARD FAILURES" IS WRONG TOO. A forge that is merely down
// will work on retry, and dropping the local record then would leave a live
// webhook delivering to a node that no longer routes it while reporting a
// cleanup that did not happen.
//
// THE LINE IS WHETHER THE FORGE ANSWERED. A 4xx means it understood the request
// and refused: the credential is gone, or the account lacks the rights, or the
// repository is not there. Repeating the identical call cannot change any of
// those — only an operator can, and an operator who is DELETING the binding has
// said they are done with that repository. A 5xx or a transport error means we
// do not know what happened, so the safe answer is to refuse and let them
// retry.
//
// Both cases were measured on the burn-in node against one orphaned binding:
// first `404 secret has been deleted` (the token was gone), and once that was
// restored, `403 user should be an owner or a collaborator with admin write`
// (the agent is not a repo admin). Neither is recoverable by retrying, and the
// binding had to go.

// ForgeRefusalIsPermanent reports whether a forge-side failure is one that
// repeating the call can never resolve, and therefore must not block removing
// the local record.
//
// THE FALSE ANSWER IS THE SAFE ONE. An error this cannot classify is treated as
// possibly transient, so the caller refuses and the operator retries — the same
// direction of caution OperatorFaultClass takes for an unknown fault class.
func ForgeRefusalIsPermanent(err error) bool {
	if err == nil {
		return false
	}
	// A binding whose credential or record is simply gone.
	if errors.Is(err, ErrNotFound) {
		return true
	}
	var apiErr *GiteaAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Status >= 400 && apiErr.Status < 500 &&
			apiErr.Status != http.StatusTooManyRequests &&
			apiErr.Status != http.StatusRequestTimeout
	}
	return false
}

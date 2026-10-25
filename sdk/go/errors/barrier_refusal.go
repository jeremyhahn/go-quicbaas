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

package errors

// systemShamirInitGuidance names the bootstrap path that replaces the system
// barrier init-shamir verb on this release.
const systemShamirInitGuidance = "barrier init-shamir is refused on this " +
	"release: the system barrier is bootstrapped by the initialization " +
	"ceremony (\"quicbaas init initialize\" or the console Ceremony page)"

// SystemShamirInitRefusedError is the one refusal every transport returns for
// the system barrier init-shamir verb. The verb stays on the API surface, but
// the system Shamir barrier is created only by the initialization ceremony.
type SystemShamirInitRefusedError struct{}

// Error implements the error interface.
func (e *SystemShamirInitRefusedError) Error() string {
	return systemShamirInitGuidance
}

// NewSystemShamirInitRefused wraps the refusal in a QBError carrying
// CodePreconditionFailed, so code classifiers and transports map it to a
// failed precondition while errors.As still finds the typed refusal.
func NewSystemShamirInitRefused(op string) *QBError {
	return &QBError{
		Code:    CodePreconditionFailed,
		Op:      op,
		Message: "system barrier init-shamir",
		Err:     &SystemShamirInitRefusedError{},
	}
}

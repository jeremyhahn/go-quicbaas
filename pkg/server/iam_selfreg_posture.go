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

package server

// selfServiceRegistrationWarning is the message logged at startup when
// iam.self_service_registration.enabled is true.
const selfServiceRegistrationWarning = "iam.self_service_registration.enabled is true: " +
	"anyone who can reach the registration endpoints can create a tenant and become its Admin"

// warnSelfServiceRegistrationPosture logs one structured warning when
// self-service tenant registration is on. The config validator can only
// refuse startup, so an operator who enables the surface on purpose would
// otherwise get no signal that it is open to the public, or whether new
// accounts are held for admin approval.
func (s *Server) warnSelfServiceRegistrationPosture() {
	reg := s.config.IAM.SelfServiceRegistration
	if !reg.Enabled {
		return
	}
	s.logger.WithFields(map[string]interface{}{
		"flag":                   "iam.self_service_registration.enabled",
		"require_admin_approval": reg.RequireAdminApproval,
	}).Warn(selfServiceRegistrationWarning)
}

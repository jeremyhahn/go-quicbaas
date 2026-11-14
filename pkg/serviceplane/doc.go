// Package serviceplane is the core of the SPEC row `services`: a service is a
// signed manifest, stored in go-qrdb and scoped to a tenant, and the seven
// lifecycle verbs (deploy, undeploy, redeploy, start, stop, resume and
// restart) move it through one state machine.
//
// The package holds the model and the rules only. Every write is arbitrated
// by the store through a Guard (go-qrdb's cas.Writer in the server), so two
// nodes racing one verb produce one winner. What a service is granted on
// deploy (a workload identity, a certificate, a registered name, RBAC from a
// built-in role and secrets) and how its containers run come in through the
// Grant and Launcher interfaces; pkg/service serves the verbs on every
// transport. Design: docs/service-plane/architecture.md.
package serviceplane

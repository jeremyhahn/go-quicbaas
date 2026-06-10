// Package dns01 re-exports go-xkms's ACME DNS-01 challenge solver.
package dns01

import xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

var ComputeChallengeValue = xkmsacme.ACMEComputeDNS01ChallengeValue

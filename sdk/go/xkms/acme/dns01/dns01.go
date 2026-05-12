// Package dns01 re-exports go-xkms's ACME DNS-01 challenge solver.
package dns01

import xkmsdns01 "github.com/jeremyhahn/go-xkms/pkg/acme/challenges/dns-01"

var ComputeChallengeValue = xkmsdns01.ComputeChallengeValue

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

package gitsign

import "strings"

// GitNamespace is the SSHSIG namespace git signs commits and tags in. It is the
// only namespace the helper signs: the helper exists to sign git objects, and
// refusing every other namespace keeps a session from using it to sign, say, a
// file or an SSH certificate request.
const GitNamespace = "git"

// stdinDataFile is the data file argument ssh-keygen reads as standard input.
const stdinDataFile = "-"

// Invocation is a validated `-Y sign` request, the subset of ssh-keygen's
// command line git uses with gpg.ssh.program:
//
//	<program> -Y sign -n git -f <public key file> [-U] [<data file>]
//
// git 2.37 and later pass -U whenever user.signingkey is a literal key, which
// is how quicbaas configures it.
type Invocation struct {
	// Namespace is the SSHSIG namespace, always GitNamespace.
	Namespace string

	// KeyFile is the public key file git wrote for the signing key.
	KeyFile string

	// DataFile is the file to sign. Empty means the data is on standard input
	// and the signature goes to standard output; otherwise the signature is
	// written to DataFile + ".sig".
	DataFile string
}

// invocationArgs collects the raw arguments before they are validated.
type invocationArgs struct {
	verb      string
	namespace string
	keyFile   string
	dataFiles []string
	seen      map[string]bool
}

// flagHandler is one ssh-keygen flag the helper accepts. A flag with a value
// stores it; a flag without one is accepted and ignored.
type flagHandler struct {
	takesValue bool
	store      func(a *invocationArgs, value string)
}

// flags are the ssh-keygen flags git passes with -Y sign. -U says the key is in
// an agent rather than a private key file, which is true of the remote key in
// the sense that matters: the helper never sees the private key. -q is quiet.
var flags = map[string]flagHandler{
	"-Y": {takesValue: true, store: func(a *invocationArgs, v string) { a.verb = v }},
	"-n": {takesValue: true, store: func(a *invocationArgs, v string) { a.namespace = v }},
	"-f": {takesValue: true, store: func(a *invocationArgs, v string) { a.keyFile = v }},
	"-U": {},
	"-q": {},
}

// verbs are the -Y operations the helper performs. Only sign: verification
// needs no private key, so stock ssh-keygen does it and git is never pointed
// at the helper for it.
var verbs = map[string]func(a *invocationArgs) (*Invocation, error){
	"sign": signInvocation,
}

// ParseInvocation validates the helper's arguments, excluding the program
// name. It returns an *InvalidInvocationError for a missing or unknown -Y
// verb, a missing -f key file, a missing or foreign -n namespace, an unknown
// flag, or more than one data file.
func ParseInvocation(args []string) (*Invocation, error) {
	a := &invocationArgs{seen: make(map[string]bool, len(flags))}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == stdinDataFile || !strings.HasPrefix(arg, "-") {
			a.dataFiles = append(a.dataFiles, arg)
			continue
		}
		flag, ok := flags[arg]
		if !ok {
			return nil, &InvalidInvocationError{Arg: arg, Message: "unsupported flag"}
		}
		if a.seen[arg] {
			return nil, &InvalidInvocationError{Arg: arg, Message: "flag given more than once"}
		}
		a.seen[arg] = true
		if !flag.takesValue {
			continue
		}
		if i+1 >= len(args) {
			return nil, &InvalidInvocationError{Arg: arg, Message: "flag needs a value"}
		}
		i++
		flag.store(a, args[i])
	}

	if a.verb == "" {
		return nil, &InvalidInvocationError{Message: "missing -Y operation"}
	}
	verb, ok := verbs[a.verb]
	if !ok {
		return nil, &InvalidInvocationError{Arg: a.verb, Message: "unsupported -Y operation; only sign is performed"}
	}
	return verb(a)
}

// signInvocation validates the arguments of -Y sign.
func signInvocation(a *invocationArgs) (*Invocation, error) {
	if a.namespace == "" {
		return nil, &InvalidInvocationError{Message: "missing -n namespace"}
	}
	if a.namespace != GitNamespace {
		return nil, &InvalidInvocationError{Arg: a.namespace, Message: "only the " + GitNamespace + " namespace is signed"}
	}
	if a.keyFile == "" {
		return nil, &InvalidInvocationError{Message: "missing -f public key file"}
	}
	if len(a.dataFiles) > 1 {
		return nil, &InvalidInvocationError{Arg: a.dataFiles[1], Message: "only one data file is signed per call"}
	}
	inv := &Invocation{Namespace: a.namespace, KeyFile: a.keyFile}
	if len(a.dataFiles) == 1 && a.dataFiles[0] != stdinDataFile {
		inv.DataFile = a.dataFiles[0]
	}
	return inv, nil
}

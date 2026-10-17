package iam

import "net/url"

// redirectURIMatches reports whether a requested redirect URI matches a
// registered one. Matching is exact string comparison, except that a
// registered http redirect URI on the loopback IP literal 127.0.0.1 or [::1]
// matches a request that differs from it only in the port: native apps bind
// an ephemeral port at request time (draft-ietf-oauth-v2-1-16 §2.3.1,
// §8.4.2). The exception does not cover localhost or any other host name.
func redirectURIMatches(registered, requested string) bool {
	if registered == requested {
		return true
	}
	registeredWithoutPort, ok := loopbackRedirectURIWithoutPort(registered)
	if !ok {
		return false
	}
	requestedWithoutPort, ok := loopbackRedirectURIWithoutPort(requested)
	return ok && registeredWithoutPort == requestedWithoutPort
}

// loopbackRedirectURIWithoutPort returns raw with its port removed when raw is
// an http URI on the loopback IP literal 127.0.0.1 or [::1].
func loopbackRedirectURIWithoutPort(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return "", false
	}
	switch u.Hostname() {
	case "127.0.0.1":
		u.Host = "127.0.0.1"
	case "::1":
		u.Host = "[::1]"
	default:
		return "", false
	}
	return u.String(), true
}

// redirectURIRegistered reports whether requested matches any of the client's
// registered redirect URIs.
func redirectURIRegistered(registered []string, requested string) bool {
	for _, uri := range registered {
		if redirectURIMatches(uri, requested) {
			return true
		}
	}
	return false
}

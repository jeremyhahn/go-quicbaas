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

package notify

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// maxRenderedBytes bounds a rendered message before the truncation marker.
const maxRenderedBytes = 4000

// truncationMarker is appended to a rendered message that was cut.
const truncationMarker = "\n…"

// Text renders n as plain text, the form Signal carries: a severity-tagged
// subject line, then the body and the link, each after a blank line.
func Text(n Notification) string {
	var b strings.Builder
	b.WriteString("[" + strings.ToUpper(string(n.Severity)) + "] " + n.Subject)
	if n.Body != "" {
		b.WriteString("\n\n" + n.Body)
	}
	if n.Link != "" {
		b.WriteString("\n\n" + n.Link)
	}
	return capRendered(b.String())
}

// slackText renders n in Slack's mrkdwn: the subject line in bold, with
// Slack's control characters escaped so a notification cannot mention
// users or channels, and the link wrapped only when it is http or https.
func slackText(n Notification) string {
	var b strings.Builder
	b.WriteString("*[" + strings.ToUpper(string(n.Severity)) + "] " + slackEscape(n.Subject) + "*")
	if n.Body != "" {
		b.WriteString("\n\n" + slackEscape(n.Body))
	}
	if n.Link != "" {
		b.WriteString("\n\n" + slackLink(n.Link))
	}
	return capRendered(b.String())
}

// slackEscape escapes &, < and > as Slack's mrkdwn requires.
func slackEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

// slackLink wraps an http or https link in Slack's link markup and escapes
// anything else as plain text.
func slackLink(link string) string {
	u, err := url.Parse(link)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") && !strings.ContainsAny(link, "<>|") {
		return "<" + link + ">"
	}
	return slackEscape(link)
}

// capRendered cuts s to maxRenderedBytes on a rune boundary and appends the
// truncation marker when it cut anything.
func capRendered(s string) string {
	if len(s) <= maxRenderedBytes {
		return s
	}
	cut := maxRenderedBytes
	floor := max(0, cut-utf8.UTFMax+1)
	for cut > floor && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + truncationMarker
}

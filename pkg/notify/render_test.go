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
	"strings"
	"testing"
	"unicode/utf8"
)

func TestText_Layout(t *testing.T) {
	got := Text(validNotification())
	want := "[WARNING] proj: build is parked\n\nQuestion: which branch?\n\nhttps://forge.example/owner/repo/issues/1"
	if got != want {
		t.Fatalf("Text:\nwant %q\ngot  %q", want, got)
	}
	bare := Text(Notification{Severity: SeverityCritical, Subject: "s"})
	if bare != "[CRITICAL] s" {
		t.Fatalf("bare Text: %q", bare)
	}
}

func TestText_TruncatesOnRuneBoundary(t *testing.T) {
	n := validNotification()
	n.Body = strings.Repeat("é", maxRenderedBytes)
	got := Text(n)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated text is not valid UTF-8")
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated text does not end in the marker")
	}
	if len(got) > maxRenderedBytes+len(truncationMarker) {
		t.Fatalf("truncated text too long: %d bytes", len(got))
	}
}

func TestSlackText_EscapesMarkup(t *testing.T) {
	n := validNotification()
	n.Subject = "a <b> & c"
	n.Body = "<!channel> & <@U1>"
	n.Link = ""
	got := slackText(n)
	want := "*[WARNING] a &lt;b&gt; &amp; c*\n\n&lt;!channel&gt; &amp; &lt;@U1&gt;"
	if got != want {
		t.Fatalf("slackText:\nwant %q\ngot  %q", want, got)
	}
}

func TestSlackText_LinkOnlyWhenHTTP(t *testing.T) {
	n := validNotification()
	n.Body = ""
	got := slackText(n)
	if !strings.HasSuffix(got, "\n\n<https://forge.example/owner/repo/issues/1>") {
		t.Fatalf("https link not wrapped: %q", got)
	}
	n.Link = "javascript:alert(1)"
	got = slackText(n)
	if strings.Contains(got, "<javascript") {
		t.Fatalf("javascript link wrapped: %q", got)
	}
	if !strings.HasSuffix(got, "\n\njavascript:alert(1)") {
		t.Fatalf("javascript link not kept as text: %q", got)
	}
}

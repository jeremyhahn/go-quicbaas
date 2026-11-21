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
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// =============================================================================
// Closing references — which issues an open pull request already closes
// =============================================================================
//
// AN ISSUE AN OPEN PULL REQUEST ALREADY CLOSES IS NOT WORK TO ADOPT. On
// 2026-09-22 pull request #1049 said "Closes #1048". The issue source adopted
// #1048 anyway, a worker was spawned for it, and that worker opened #1052, a
// duplicate of the change already under review (go-quicbaas#1054).
//
// THE KEYWORDS ARE WHERE THE FORGE'S LINK COMES FROM. Gitea derives its
// issue-to-pull-request closing link from these keywords in the pull request's
// title and body. Reading them from the one listing the sweep already makes
// answers the same question without a timeline read per issue.
//
// ONLY THIS REPOSITORY'S ISSUES. A qualified reference to another repository
// closes an issue there, and an issue here with the same number is a different
// piece of work.

// closingReferencePattern matches one closing keyword and the issue it names:
// "#12", "owner/repo#12", or an issue URL. Case-insensitive, and the keyword
// must be a whole word, so "prefixes #3" closes nothing.
var closingReferencePattern = regexp.MustCompile(
	`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\b:?[ \t]*` +
		`(?:(?:([\w.-]+)/([\w.-]+))?#(\d+)|(https?://[^\s<>()\[\]]+?)/issues/(\d+))\b`)

// ClosingIssueNumbers returns the issues of repo that text closes with a
// closing keyword, ascending and without duplicates.
//
// repoURL is the repository's web address on the bound forge
// ("https://gitea.test/acme/api"). A URL reference counts only when it is under
// that address: `https://github.com/acme/api/issues/25` is not Gitea's
// acme/api#25. An empty repoURL admits no URL reference at all.
func ClosingIssueNumbers(repo RepoRef, repoURL, text string) []int {
	local := schemeless(strings.TrimSpace(repoURL))
	seen := map[int]bool{}
	for _, match := range closingReferencePattern.FindAllStringSubmatch(text, -1) {
		number := match[3]
		switch {
		case number != "" && match[1] != "" && !sameRepo(repo, RepoRef{Owner: match[1], Repo: match[2]}):
			continue
		case number == "":
			if local == "" || !strings.EqualFold(schemeless(match[4]), local) {
				continue
			}
			number = match[5]
		}
		n, err := strconv.Atoi(number)
		if err != nil || n <= 0 {
			continue
		}
		seen[n] = true
	}
	numbers := make([]int, 0, len(seen))
	for n := range seen {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	return numbers
}

// repoWebURL is the repository's web address as the forge itself reports it,
// read off an issue's own URL ("<repo web address>/issues/<n>"). The binding
// names only the API root, and the forge's answer is better than a guess at
// how that root maps to the web one. Empty when no issue carries a URL of
// that shape.
func repoWebURL(issues []ForgeIssue) string {
	for i := range issues {
		suffix := "/issues/" + strconv.Itoa(issues[i].Number)
		if prefix, ok := strings.CutSuffix(strings.TrimSpace(issues[i].URL), suffix); ok && prefix != "" {
			return prefix
		}
	}
	return ""
}

// schemeless drops an http or https scheme and any trailing slash. The same
// forge answers on both schemes, so a link written with the other one still
// names the same issue.
func schemeless(address string) string {
	if _, rest, ok := strings.Cut(address, "://"); ok {
		address = rest
	}
	return strings.TrimRight(address, "/")
}

// closingPullRequests maps every issue of repo that one of pulls closes to the
// pull request that closes it. When two close the same issue, the
// lowest-numbered one is named, so the answer, and the audit record that
// quotes it, is the same on every sweep.
func closingPullRequests(repo RepoRef, repoURL string, pulls []ForgePullRequest) map[int]int {
	closing := map[int]int{}
	for _, pull := range pulls {
		for _, issue := range ClosingIssueNumbers(repo, repoURL, pull.Title+"\n"+pull.Body) {
			if held, ok := closing[issue]; !ok || pull.Number < held {
				closing[issue] = pull.Number
			}
		}
	}
	return closing
}

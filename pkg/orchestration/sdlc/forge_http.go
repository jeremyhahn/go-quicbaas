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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// Forge HTTP core
// =============================================================================
//
// What every REST forge adapter shares: one request shape, one attempt (send),
// credential custody on the wire, body bounds and scrubbing, and the error
// shapes a non-2xx answer becomes. What differs between forges — the
// Authorization scheme, the media type, and the few answers a forge gives a
// meaning of its own — is a forgeDialect, so each adapter states only those.

// forgeRequest is one API call.
type forgeRequest struct {
	method string
	path   string
	query  url.Values

	// body is marshalled as JSON when non-nil.
	body any

	// out receives the decoded response when non-nil.
	out any

	// raw receives the response body when non-nil, for the routes that answer
	// in something other than JSON. It is a SCRUBBED COPY — the adapter's own
	// buffer is zeroed on the way out, and a body that outlives the call must
	// carry no credential (see send).
	raw *[]byte

	// next receives the URL of the following page when non-nil, from the
	// response's Link header (RFC 8288, rel="next"). Empty means there is no
	// next page. Only forges that page by link set it.
	next *string

	// header receives a successful response's headers when non-nil, for a
	// forge that states its paging in headers of its own rather than (or as
	// well as) a Link header.
	header *http.Header

	// maxBody bounds what a response may cost in memory and what the caller
	// receives, keeping the LAST maxBody bytes of a longer one. Zero — every
	// call but a job log — reads the body whole.
	//
	// The tail rather than a prefix because the only route that sets this is
	// the job log, and a CI log's answer is at its end. send reads slightly
	// past the bound so the scrub cannot meet a half credential; the caller
	// still gets at most maxBody.
	maxBody int

	// operation names the call for error attribution, and keys its entry in
	// forgeWritePolicies.
	operation string

	// auth selects the credential. The zero value is the adapter's own token,
	// so every workflow call is unchanged by the custody plane existing.
	auth forgeAuth

	// subjectToken is the token to authenticate AS when auth is
	// authSubjectToken. Never logged, never placed in an error. A borrowed
	// byte view under the caller's no-retain contract: forgeRequest is
	// per-call and never stored, and send converts it to the string the HTTP
	// header requires only transiently (go-quicbaas#97 custody debt).
	subjectToken []byte

	// bodySecrets are secrets the request body sends — a webhook's token —
	// which a refusal echoing that body must not carry into an error. Like
	// subjectToken they are the caller's, and never stored.
	bodySecrets [][]byte
}

// secrets lists the caller's secrets the call carries — the subject token and
// any the body sends — for send to scrub from what the forge answers.
func (r forgeRequest) secrets() [][]byte {
	return append([][]byte{r.subjectToken}, r.bodySecrets...)
}

// forgeAuth selects which credential authenticates a request.
type forgeAuth int

const (
	// authAdapterToken uses the adapter's own token. The default.
	authAdapterToken forgeAuth = iota

	// authCustody uses the provisioning-held basic credential.
	//
	// Required for minting and revoking on Gitea: its token endpoints are not
	// reachable with the token being replaced, so rotation runs on a
	// password-equivalent held by the provisioning plane.
	authCustody

	// authSubjectToken authenticates as a token supplied per call.
	//
	// This exists for verification and nothing else. Proving a replacement
	// works means using IT — checking with the credential that minted it would
	// prove only that minting succeeded.
	authSubjectToken
)

// forgeDialect is what one forge's REST API does differently on the wire.
type forgeDialect struct {
	// tokenScheme prefixes a token in the Authorization header, space
	// included.
	tokenScheme string

	// headers are set on every request: the media type, and any API version
	// pin.
	headers map[string]string

	// refusal gives a non-2xx answer a forge-specific meaning, or returns nil
	// to leave it to the generic mapping. It never sees a 404 or a gateway
	// status, which mean the same thing on every forge. body is already
	// scrubbed.
	refusal func(req forgeRequest, status int, body []byte) error

	// duplicatePullStatus is the status the forge refuses a pull request
	// create with when one is already open for the same refs, so
	// openPullRequest adopts the existing one instead of failing.
	duplicatePullStatus int

	// pageSize is the per_page forgePages asks for: the forge's maximum, so
	// a listing costs as few requests against the rate limit as it can.
	pageSize int

	// pageHeader names the response header a forge states the next page
	// number in, or is empty for a forge that pages by Link header alone.
	pageHeader string
}

// forgeHTTP is the HTTP client an adapter embeds.
type forgeHTTP struct {
	// bodySeam, when non-nil (tests only), observes each response body
	// buffer AFTER send's deferred wipe — the seam that makes the wipe
	// assertable. Production leaves it nil.
	bodySeam func(body []byte)

	forge     ForgeType
	dialect   forgeDialect
	baseURL   string
	token     string
	custody   CustodyCredential
	client    *http.Client
	transport ForgeTransport
	waiter    RetryWaiter
}

// forgeHTTPConfig is what newForgeHTTP validates and resolves.
type forgeHTTPConfig struct {
	baseURL   string
	token     string
	custody   CustodyCredential
	client    *http.Client
	transport ForgeTransport
	waiter    RetryWaiter
}

// newForgeHTTP validates an adapter's configuration and resolves its defaults.
func newForgeHTTP(forge ForgeType, dialect forgeDialect, cfg forgeHTTPConfig) (forgeHTTP, error) {
	base := strings.TrimRight(cfg.baseURL, "/")
	if base == "" {
		return forgeHTTP{}, &InvalidInputError{Field: "base_url", Message: "must not be empty"}
	}
	if _, err := url.Parse(base); err != nil {
		return forgeHTTP{}, &InvalidInputError{Field: "base_url", Message: "is not a valid URL"}
	}
	if cfg.token == "" {
		return forgeHTTP{}, &InvalidInputError{
			Field:   "token",
			Message: "must not be empty; an unauthenticated adapter can only fail on first use",
		}
	}

	if err := cfg.transport.Validate(); err != nil {
		return forgeHTTP{}, err
	}
	transport := cfg.transport.Resolved()

	// NO CLIENT-WIDE TIMEOUT: each call carries its own deadline (see do),
	// because a single bound is too long for a listing and too short for a
	// squash merge of a large repository.
	client := cfg.client
	if client == nil {
		client = &http.Client{}
	}
	waiter := cfg.waiter
	if waiter == nil {
		waiter = backoffWaiter{base: transport.RetryBackoff}
	}
	return forgeHTTP{
		forge: forge, dialect: dialect, baseURL: base, token: cfg.token, custody: cfg.custody,
		client: client, transport: transport, waiter: waiter,
	}, nil
}

// scrubCredentials removes any credential this call could have sent from
// bytes that are about to become an error message. BYTE-WISE (review 991):
// the old string round-trips minted unwipeable copies of secret-bearing
// regions; this one allocates only []byte generations and LOOP-ZEROES every
// intermediate it creates — the needle copies of the adapter credentials and
// each partially-scrubbed generation — before returning. The input body is
// NOT wiped here (the caller owns it and wipes it on every exit); the
// returned slice contains no secrets and is safe to outlive the call.
func (h *forgeHTTP) scrubCredentials(body []byte, secrets ...[]byte) []byte {
	redacted := []byte("[redacted]")
	scrubbed := body
	scrub := func(secret []byte) {
		if len(secret) == 0 {
			return
		}
		next := bytes.ReplaceAll(scrubbed, secret, redacted)
		// bytes.ReplaceAll always returns a fresh slice, so a superseded
		// generation this function allocated is wiped the moment it is
		// replaced. The original body (generation zero) is the caller's.
		if len(scrubbed) != 0 && &scrubbed[0] != &body[0] {
			for i := range scrubbed {
				scrubbed[i] = 0
			}
		}
		scrubbed = next
	}
	for _, cred := range []string{h.token, h.custody.Password} {
		if cred == "" {
			continue
		}
		needle := []byte(cred)
		scrub(needle)
		for i := range needle {
			needle[i] = 0
		}
	}
	for _, secret := range secrets {
		scrub(secret)
	}
	if len(scrubbed) != 0 && &scrubbed[0] == &body[0] {
		// No replacement ever happened: hand back a copy so the caller's
		// wipe of body cannot reach into the error message.
		out := make([]byte, len(scrubbed))
		copy(out, scrubbed)
		return out
	}
	return scrubbed
}

// send performs one attempt at an API call, bounded by deadline, and decodes
// its response. do decides whether an unanswered attempt is sent again.
func (h *forgeHTTP) send(ctx context.Context, req forgeRequest, deadline time.Duration) error {
	attemptCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	endpoint := h.baseURL + req.path
	if len(req.query) > 0 {
		endpoint += "?" + req.query.Encode()
	}

	var payload io.Reader
	if req.body != nil {
		encoded, err := json.Marshal(req.body)
		if err != nil {
			return backendError(req.operation+".encode", err)
		}
		payload = bytes.NewReader(encoded)
	}

	httpReq, err := http.NewRequestWithContext(attemptCtx, req.method, endpoint, payload)
	if err != nil {
		return backendError(req.operation+".request", err)
	}
	// No credential is ever logged or reaches an error.
	switch req.auth {
	case authCustody:
		httpReq.SetBasicAuth(h.custody.Username, h.custody.Password)
	case authSubjectToken:
		// THE NET/HTTP BOUNDARY, request side: header values are strings by
		// API — there is no []byte path — so this conversion is constructed
		// at the last possible moment, inside the one call that sends it,
		// and nothing retains it beyond the request's round-trip. It is the
		// same floor already stated on the response side (the body wipe
		// below): what net/http holds — this header string included — is
		// memory this code never owns. Replacing it means an HTTP client
		// abstraction over secure buffers: tracked on go-quicbaas#97.
		httpReq.Header.Set("Authorization", h.dialect.tokenScheme+string(req.subjectToken))
	case authAdapterToken:
		fallthrough
	default:
		// The default is deliberate rather than incidental: an unrecognised
		// mode must still authenticate as somebody, and the adapter's own
		// token is the least-surprising choice. Sending nothing would turn a
		// programming slip into an anonymous request the forge might accept.
		httpReq.Header.Set("Authorization", h.dialect.tokenScheme+h.token)
	}
	for name, value := range h.dialect.headers {
		httpReq.Header.Set(name, value)
	}
	if req.body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return h.unanswered(ctx, req.operation, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read the body before branching: an error response carries the forge's
	// own message, and discarding it leaves an operator with a bare status
	// code.
	//
	// READ PAST THE BOUND BY ONE CREDENTIAL'S WIDTH. The scrub below matches
	// whole credentials, so a token that straddles the cut would lose its head
	// to the tail read and its remainder would survive the scrub as an
	// unrecognisable suffix — a partial secret in a body that outlives the
	// call. Reading the margin and trimming AFTER the scrub means every
	// credential that touches the retained region is retained whole, and so
	// redacted whole.
	readBound := req.maxBody
	if readBound > 0 {
		readBound += h.credentialWidth(req.subjectToken)
	}
	rawBody, readErr := readBodyTail(resp.Body, readBound)
	if readErr != nil {
		return h.unanswered(ctx, req.operation, readErr)
	}
	// THE RAW BODY IS ADAPTER-OWNED, SECRET-BEARING MEMORY (review 991): a
	// mint response carries the token, and any response may echo a
	// credential back. It is LOOP-ZEROED on every exit — success, error,
	// panic — after the success decode or the scrub has copied out what
	// outlives the call. With this wipe the adapter's documented residue
	// shrinks to what net/http's internals transiently held while reading
	// the response — memory this code never owns, the genuine #97
	// secure-transport floor. bodySeam (tests only) observes the buffer
	// AFTER the wipe.
	defer func() {
		for i := range rawBody {
			rawBody[i] = 0
		}
		if h.bodySeam != nil {
			h.bodySeam(rawBody)
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		return &NotFoundError{Entity: string(h.forge) + " " + req.operation, ID: req.path}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// SCRUB THE CREDENTIALS OUT OF THE BODY before it can reach a typed
		// error. The forge's message is relayed to operators and stored, and
		// a server or a proxy in front of it may echo the Authorization
		// header or the token back in its response. Redacting by VALUE is
		// possible precisely here, because this is the one place that holds
		// every credential — anywhere further out would be guessing at what a
		// secret looks like. The scrubbed copy is secret-free and may outlive
		// the call inside an error; the raw body above never leaves it.
		return h.refused(req, resp.StatusCode, h.scrubCredentials(rawBody, req.secrets()...))
	}

	if req.next != nil {
		*req.next = nextLink(resp.Header.Values("Link"))
	}
	if req.header != nil {
		*req.header = resp.Header.Clone()
	}
	if req.raw != nil {
		// SCRUBBED, NOT MERELY COPIED, and by the rule stated for the error
		// path: a body that OUTLIVES the call must be secret-free, because
		// nothing further out holds every credential to redact by value. It
		// matters more here than there — a job log is quoted into a review
		// record and into a worker prompt, which is further than an error
		// message ever travels — and scrubCredentials returns adapter-owned
		// memory either way, so the deferred wipe of rawBody cannot reach it.
		scrubbed := h.scrubCredentials(rawBody, req.secrets()...)
		if req.maxBody > 0 && len(scrubbed) > req.maxBody {
			// Now the caller's bound, applied to a body with nothing left in
			// it to cut in half. Redaction only ever shortens, so this is the
			// margin coming back off.
			scrubbed = scrubbed[len(scrubbed)-req.maxBody:]
		}
		*req.raw = scrubbed
	}
	if req.out == nil || len(rawBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(rawBody, req.out); err != nil {
		return backendError(req.operation+".decode", err)
	}
	return nil
}

// refused maps a non-2xx, non-404 answer to its typed error. body is scrubbed.
func (h *forgeHTTP) refused(req forgeRequest, status int, body []byte) error {
	apiErr := &ForgeAPIError{Type: h.forge, Status: status, Operation: req.operation, Message: forgeMessage(body)}
	if forgeGatewayStatuses[status] {
		return &ForgeUnansweredError{Type: h.forge, Operation: req.operation, Cause: apiErr}
	}
	if h.dialect.refusal != nil {
		if err := h.dialect.refusal(req, status, body); err != nil {
			return err
		}
	}
	return backendError(req.operation, apiErr)
}

// credentialWidth is the longest credential this call could have sent.
//
// The margin a bounded read has to overshoot by so no secret can be cut in
// half by the bound; see send. A LENGTH is all this computes — no copy of any
// credential is made, and none can be inferred from the number beyond what the
// scheme already fixes.
func (h *forgeHTTP) credentialWidth(subjectToken []byte) int {
	widest := len(subjectToken)
	for _, cred := range []string{h.token, h.custody.Password} {
		if len(cred) > widest {
			widest = len(cred)
		}
	}
	return widest
}

// readBodyTail reads a response body, keeping at most max bytes of its END.
//
// max <= 0 reads the whole body — the behaviour every caller but the job log
// has always had. Above zero it streams through a fixed pair of buffers and
// never holds more than 2*max, so a hundred-megabyte CI log costs the sweep
// the bound and not the log.
func readBodyTail(body io.Reader, max int) ([]byte, error) {
	if max <= 0 {
		return io.ReadAll(body)
	}
	// Two buffers, filled alternately: when the newer one is full the older
	// is discarded, so the pair always spans the last max..2*max bytes read
	// and the tail is a suffix of their concatenation.
	older := make([]byte, 0, max)
	newer := make([]byte, 0, max)
	chunk := make([]byte, 32*1024)
	for {
		n, err := body.Read(chunk)
		if n > 0 {
			for read := chunk[:n]; len(read) > 0; {
				room := max - len(newer)
				if room == 0 {
					older, newer = newer, older[:0]
					continue
				}
				if room > len(read) {
					room = len(read)
				}
				newer = append(newer, read[:room]...)
				read = read[room:]
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if len(newer) >= max || len(older) == 0 {
		return newer, nil
	}
	// The newer buffer alone is short of the bound; take the rest from the
	// end of the older one.
	tail := make([]byte, 0, max)
	tail = append(tail, older[len(older)-(max-len(newer)):]...)
	return append(tail, newer...), nil
}

// nextLink returns the rel="next" target of a response's Link header values,
// or "" when there is none.
func nextLink(values []string) string {
	for _, value := range values {
		for _, link := range strings.Split(value, ",") {
			target, params, found := strings.Cut(strings.TrimSpace(link), ";")
			if !found || !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
				continue
			}
			for _, param := range strings.Split(params, ";") {
				name, val, _ := strings.Cut(strings.TrimSpace(param), "=")
				if strings.EqualFold(name, "rel") && strings.Trim(val, `"`) == "next" {
					return target[1 : len(target)-1]
				}
			}
		}
	}
	return ""
}

// errForeignPageLink reports a Link header whose next page lies outside the
// configured API root.
//
// The adapter sends its token with every request, so following a link to any
// other host would hand the credential to whoever wrote the header.
var errForeignPageLink = errors.New(
	"next-page link points outside the configured API root")

// followLink splits a next-page URL into the path and query send expects,
// refusing one that does not lie under the configured API root.
func (h *forgeHTTP) followLink(operation, next string) (string, url.Values, error) {
	rest, ok := strings.CutPrefix(next, h.baseURL+"/")
	if !ok || strings.HasPrefix(rest, "/") {
		return "", nil, backendError(operation, errForeignPageLink)
	}
	target, err := url.Parse("/" + rest)
	if err != nil || target.Host != "" || target.User != nil {
		return "", nil, backendError(operation, errForeignPageLink)
	}
	return target.EscapedPath(), target.Query(), nil
}

// forgeMessage extracts the forge's error message, falling back to the raw
// body. Gitea and GitHub both answer an error with a JSON "message".
//
// Bounded: an error body is remote input, and an unbounded one would put an
// arbitrary amount of someone else's text into a log line.
func forgeMessage(body []byte) string {
	var envelope struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Message != "" {
		return truncate(envelope.Message, 200)
	}
	return truncate(strings.TrimSpace(string(body)), 200)
}

// truncate bounds s to n bytes, marking it when it was cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ForgeAPIError reports a forge response the adapter could not use. It is
// reported as the cause inside a BackendError.
type ForgeAPIError struct {
	Type      ForgeType
	Status    int
	Operation string
	Message   string
}

func (e *ForgeAPIError) Error() string {
	msg := string(e.Type) + " " + e.Operation + ": HTTP " + strconv.Itoa(e.Status)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

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

package orchestration

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
)

// =============================================================================
// How much prompt a served model will accept
// =============================================================================
//
// EVERY MODEL HAS A LIMIT AND ALMOST NO SERVER WILL TELL YOU WHAT IT IS.
// Measured against OpenVINO Model Server 2026.3.1: /v3/models and
// /v1/models/{name} return id, object, created and owned_by, and
// /v1/models/{name}/metadata answers 500. There is nothing to read. Other
// OpenAI-compatible servers expose something (a max_model_len, an n_ctx, a
// max_input_length) but they do not agree on where or what it is called, and a
// caller that depends on any one of them works against one server.
//
// So the limit is established by a LADDER, in descending order of trust, and
// every rung is optional:
//
//	declared   the profile states it. Portable by construction: it is a number
//	           an operator knows about their own deployment, and it is the only
//	           rung that can be right BEFORE the first request.
//	learned    a refusal taught us. Costs nothing until something overruns, and
//	           needs no knowledge of the server at all.
//	unbounded  nothing is known; send it and let the server decide.
//
// WHAT IS DELIBERATELY NOT HERE. No model geometry, no KV-cache arithmetic, no
// config.json parsing: layer and head counts are a transformer-family
// convention, not a universal one, and a backend that computes a limit from
// them is wrong the first time it meets a model shaped differently. No
// per-vendor metadata probes either — they would each be a guess about one
// server wearing the costume of a general mechanism.

// ContextLimit tracks what a served model will accept, from a declaration and
// from what refusals have taught it.
//
// The zero value is usable and means "nothing known": Allows reports true for
// every size until a refusal narrows it.
type ContextLimit struct {
	// declared is the operator's own number; zero means they did not say.
	declared int64

	// learned is the smallest size the server has REFUSED: an exclusive upper
	// bound, because the size that failed is by definition too large. Zero
	// means nothing has been refused yet.
	learned atomic.Int64

	// ratio and samples relate this package's estimate to the server's own
	// token count, from responses that carried one. See Calibrate.
	ratio   atomicFloat64
	samples atomic.Int64
}

// atomicFloat64 is a lock-free float, which atomic has no native type for.
// Values are exchanged as their IEEE-754 bit patterns.
type atomicFloat64 struct{ bits atomic.Uint64 }

func (f *atomicFloat64) Load() float64   { return math.Float64frombits(f.bits.Load()) }
func (f *atomicFloat64) Store(v float64) { f.bits.Store(math.Float64bits(v)) }

// NewContextLimit returns a limit carrying an operator's declaration. Zero or
// negative declares nothing, which is not an error: most deployments do not
// know their number and are perfectly well served by learning it.
func NewContextLimit(declared int) *ContextLimit {
	limit := &ContextLimit{}
	if declared > 0 {
		limit.declared = int64(declared)
	}
	return limit
}

// DefaultTokenRatio is the real-tokens-per-estimated-token factor assumed
// before any response has been seen.
//
// ONE, BECAUSE THE ESTIMATOR IS ALREADY TRYING TO COUNT TOKENS. The
// four-characters-per-token rule it uses approximates real tokens directly, so
// the honest prior is that they are the same number. Calibrate refines it from
// what the server actually reports.
const DefaultTokenRatio = 1.0

// Bound returns the tightest known ceiling in ESTIMATE units, or zero when
// nothing is known.
//
// THE TWO RUNGS ARE IN DIFFERENT UNITS AND ARE CONVERTED, NOT COMPARED
// NAIVELY. A learned bound is a number this package measured with
// EstimateTokens, so it is already in estimate units. A DECLARED bound is what
// an operator knows about their model, which is real tokens — and a prompt's
// real token count cannot be known before sending it, because the tokenizer
// belongs to the served model. So the declaration is divided by the observed
// ratio to express it in the same unit as everything else. Comparing them
// directly would silently mean two different things by one number.
//
// THE LEARNED BOUND WINS WHEN IT IS TIGHTER. An operator who declares 32768
// against a server compiled for 1024 is wrong, and the server is the authority
// on its own configuration; trusting the declaration there would refuse
// nothing and fail every request instead.
func (c *ContextLimit) Bound() int {
	if c == nil {
		return 0
	}
	learned := int(c.learned.Load())
	declared := 0
	if c.declared > 0 {
		ratio := c.Ratio()
		if ratio <= 0 {
			ratio = DefaultTokenRatio
		}
		declared = int(float64(c.declared) / ratio)
	}
	switch {
	case learned > 0 && declared > 0:
		return min(learned, declared)
	case declared > 0:
		return declared
	default:
		return learned
	}
}

// Allows reports whether a prompt of this estimated size is worth sending.
//
// UNKNOWN ALLOWS EVERYTHING. A limit that refused what it had not measured
// would turn "we have not learned anything yet" into an outage.
func (c *ContextLimit) Allows(estimated int) bool {
	bound := c.Bound()
	return bound <= 0 || estimated < bound
}

// Refused records that a prompt of this size was rejected for being too long,
// and reports whether that narrowed what was known.
//
// IT ONLY EVER RATCHETS DOWN. A later refusal at a larger size says nothing new
// — the smallest refusal is the tightest evidence — and letting the bound rise
// would make a single anomalous rejection widen the ceiling rather than narrow
// it.
func (c *ContextLimit) Refused(tokens int) bool {
	if c == nil || tokens <= 0 {
		return false
	}
	for {
		current := c.learned.Load()
		if current > 0 && current <= int64(tokens) {
			return false
		}
		if c.learned.CompareAndSwap(current, int64(tokens)) {
			return true
		}
	}
}

// =============================================================================
// Recognising a refusal for length
// =============================================================================

// lengthRefusalStatuses are the statuses a server uses to say the request was
// too large. Anything else is a different failure and must not narrow the
// bound: treating a 500 as a length refusal would teach the ceiling a number
// from an outage.
var lengthRefusalStatuses = map[int]bool{
	400: true, // OpenAI-compatible servers, including OVMS
	413: true, // payload too large
	422: true, // unprocessable, used by some gateways
}

// lengthRefusalSubjects and lengthRefusalVerdicts are the two halves a message
// must have to be read as "too long". Requiring one of EACH is what keeps this
// from matching unrelated 400s: "invalid token" is a credential error and has
// a subject with no verdict, while "model exceeded its quota" has a verdict
// with no subject.
var (
	lengthRefusalSubjects = []string{"prompt", "context", "input", "token", "sequence", "message"}
	lengthRefusalVerdicts = []string{
		"exceed", "too long", "too large", "too many",
		"maximum", "max_", "longer than", "less than", "at most", "limit",
	}
)

// IsLengthRefusal reports whether a server response means the prompt was too
// long for the model.
//
// IT IS A HEURISTIC OVER TEXT, AND IT IS THE HONEST ONE. There is no status
// code, header or error code that OpenAI-compatible servers agree on for this;
// OVMS answers 400 with a mediapipe stack trace, and every other server has its
// own phrasing. Matching a subject AND a verdict recognises all of them without
// claiming to know any particular one — and a false negative costs only a
// ceiling that stays unlearned, which is where it started.
func IsLengthRefusal(status int, body string) bool {
	if !lengthRefusalStatuses[status] {
		return false
	}
	lower := strings.ToLower(body)
	return containsAny(lower, lengthRefusalSubjects) && containsAny(lower, lengthRefusalVerdicts)
}

func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

// =============================================================================
// Estimating a prompt's size without a tokenizer
// =============================================================================

// EstimateTokens approximates how many tokens a string will become.
//
// A BACKEND CANNOT TOKENIZE WHAT IT DOES NOT HOST. The tokenizer belongs to the
// served model, and shipping one per model family is exactly the coupling this
// file exists to avoid — so the estimate is deliberately crude and its only job
// is to be comparable with ITSELF: the number that was refused and the number
// about to be sent are produced the same way, so the ratchet is sound even when
// the ratio to real tokens is not one.
//
// The divisor is the widely used four-characters-per-token rule of thumb for
// English prose and code. It under-counts dense CJK and over-counts long runs
// of whitespace; both are acceptable because a server's own count is available
// on every SUCCESSFUL response and calibrates this one (see Calibrate).
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	estimate := (len(text) + 3) / 4
	if estimate < 1 {
		return 1
	}
	return estimate
}

// Calibrate teaches the limit the ratio between this estimator and the
// server's own count, using a response that SUCCEEDED and reported one.
//
// WHY IT IS WORTH DOING. The bound is learned in estimated tokens and enforced
// in estimated tokens, so it is self-consistent — but an operator's DECLARED
// bound is in real tokens, and comparing the two without a ratio mixes units.
// A single successful response is enough to relate them, and every
// OpenAI-compatible server reports usage.prompt_tokens on success, which is the
// one piece of metadata they do all agree on.
//
// IT IS A RATIO AND THE OVERHEAD IS NOT. A chat template contributes a fixed
// number of tokens the estimator cannot see, so a SHORT prompt calibrates high
// — measured at 2.5 against a real count of 10 on a six-character prompt,
// where the template was most of the total. That errs toward a tighter bound,
// which is the safe direction, and the distortion shrinks to nothing as
// prompts grow, which is where the limit matters.
//
// A zero or absent count teaches nothing and is not an error: the response
// simply did not carry one.
func (c *ContextLimit) Calibrate(estimated, reported int) {
	if c == nil || estimated <= 0 || reported <= 0 {
		return
	}
	ratio := c.ratio.Load()
	// A simple running mean over observations keeps one outlier — a response
	// whose prompt was mostly a pasted binary, say — from moving the ratio far.
	if ratio == 0 {
		c.ratio.Store(float64(reported) / float64(estimated))
		c.samples.Add(1)
		return
	}
	n := float64(c.samples.Add(1))
	c.ratio.Store(ratio + (float64(reported)/float64(estimated)-ratio)/n)
}

// Ratio returns the learned real-tokens-per-estimated-token factor, or zero
// when nothing has been observed.
func (c *ContextLimit) Ratio() float64 {
	if c == nil {
		return 0
	}
	return c.ratio.Load()
}

// =============================================================================
// The refusal, as an error a caller can act on
// =============================================================================

// ErrPromptTooLong indicates a prompt exceeded what the served model accepts.
//
// IT IS SEPARATE FROM EVERY OTHER SERVER ERROR because the remedies are
// different and a caller must be able to tell them apart without reading
// prose. A prompt too long is fixed by raising the server's limit or by
// sending less, and it will fail identically on every retry; an unreachable
// endpoint or a 500 is transient and worth retrying. Charging a retry budget
// against the first is how an attempt budget is spent for nothing.
var ErrPromptTooLong = errors.New("agent backend: prompt exceeds the model's context")

// PromptTooLongError reports a prompt the served model will not accept. It
// names BOTH sizes, because "too long" without them tells an operator nothing
// they can act on — not whether to raise the server limit, and not by how much.
type PromptTooLongError struct {
	// Backend and Model identify what refused it.
	Backend string
	Model   string

	// Estimated is the prompt's size as this package measures it; see
	// EstimateTokens for why it is an estimate and what it is comparable with.
	Estimated int

	// Bound is the tightest ceiling known at the time, or zero when the
	// refusal is what established it.
	Bound int

	// URL and Status describe the exchange. Status is zero when the request
	// was refused locally, before it was sent.
	URL    string
	Status int

	// Detail is the server's own words, truncated. Empty for a local refusal.
	Detail string
}

// Error implements the error interface.
func (e *PromptTooLongError) Error() string {
	msg := ErrPromptTooLong.Error() + ": " + e.Backend
	if e.Model != "" {
		msg += "/" + e.Model
	}
	msg += " refused about " + strconv.Itoa(e.Estimated) + " estimated token(s)"
	if e.Bound > 0 {
		msg += "; the endpoint accepts fewer than " + strconv.Itoa(e.Bound)
	}
	if e.Status == 0 {
		msg += " (refused locally against a previously measured limit; raise the server's context or send less)"
	} else if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PromptTooLongError) Unwrap() error { return ErrPromptTooLong }

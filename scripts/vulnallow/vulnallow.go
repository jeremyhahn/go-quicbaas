package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

// advisoryPrefix starts every Go vulnerability database ID.
const advisoryPrefix = "GO-"

// Entry is one allowlist line: an advisory ID and the reason it is allowed.
type Entry struct {
	ID     string
	Reason string
	File   string
	Line   int
}

// Allowed is a reachable advisory the allowlist permits.
type Allowed struct {
	ID     string
	Reason string
}

// Finding is a reachable advisory that is not on the allowlist.
type Finding struct {
	ID           string
	Summary      string
	Module       string
	Version      string
	FixedVersion string
	Position     string
}

// Result is the allowlist applied to one govulncheck stream.
type Result struct {
	Allowed  []Allowed
	Unlisted []Finding
	Stale    []Entry
	// Fixable holds listed advisories govulncheck now reports a fixed release
	// for; the entry no longer qualifies.
	Fixable []Finding
}

// Failed reports whether any reachable advisory is unlisted or any entry is stale.
func (r Result) Failed() bool {
	return len(r.Unlisted) > 0 || len(r.Stale) > 0 || len(r.Fixable) > 0
}

// MissingReasonError is an allowlist entry with an ID and no reason.
type MissingReasonError struct {
	File string
	Line int
	ID   string
}

func (e *MissingReasonError) Error() string {
	return "vulnallow: " + e.File + ":" + strconv.Itoa(e.Line) + ": " + e.ID + " has no reason"
}

// BadIDError is an allowlist entry whose ID is not a Go advisory ID.
type BadIDError struct {
	File string
	Line int
	ID   string
}

func (e *BadIDError) Error() string {
	return "vulnallow: " + e.File + ":" + strconv.Itoa(e.Line) + ": " + e.ID +
		" is not a Go advisory ID (it must start with " + advisoryPrefix + ")"
}

// DuplicateIDError is an allowlist entry whose ID an earlier line already lists.
type DuplicateIDError struct {
	File      string
	Line      int
	ID        string
	FirstLine int
}

func (e *DuplicateIDError) Error() string {
	return "vulnallow: " + e.File + ":" + strconv.Itoa(e.Line) + ": " + e.ID +
		" is already listed on line " + strconv.Itoa(e.FirstLine)
}

// AllowlistReadError is a failure to read the allowlist.
type AllowlistReadError struct {
	File string
	Err  error
}

func (e *AllowlistReadError) Error() string {
	return "vulnallow: read " + e.File + ": " + e.Err.Error()
}

func (e *AllowlistReadError) Unwrap() error { return e.Err }

// StreamError is a failure to decode govulncheck's JSON stream.
type StreamError struct {
	Err error
}

func (e *StreamError) Error() string {
	return "vulnallow: decode govulncheck JSON: " + e.Err.Error()
}

func (e *StreamError) Unwrap() error { return e.Err }

// message is the part of a govulncheck -format json message this tool reads;
// every other message kind (config, progress, SBOM) decodes to nil fields.
type message struct {
	OSV     *osvEntry `json:"osv"`
	Finding *finding  `json:"finding"`
}

type osvEntry struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

type finding struct {
	OSV          string  `json:"osv"`
	FixedVersion string  `json:"fixed_version"`
	Trace        []frame `json:"trace"`
}

type frame struct {
	Module   string    `json:"module"`
	Version  string    `json:"version"`
	Package  string    `json:"package"`
	Function string    `json:"function"`
	Receiver string    `json:"receiver"`
	Position *position `json:"position"`
}

type position struct {
	Filename string `json:"filename"`
	Line     int    `json:"line"`
}

// ParseAllowlist reads one `<GO-ID> <reason>` entry per line, skipping blank
// lines and lines that start with #.
func ParseAllowlist(file string, r io.Reader) ([]Entry, error) {
	var entries []Entry
	firstLine := make(map[string]int)
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, reason, _ := strings.Cut(line, " ")
		reason = strings.TrimSpace(reason)
		if !strings.HasPrefix(id, advisoryPrefix) {
			return nil, &BadIDError{File: file, Line: lineNo, ID: id}
		}
		if reason == "" {
			return nil, &MissingReasonError{File: file, Line: lineNo, ID: id}
		}
		if first, ok := firstLine[id]; ok {
			return nil, &DuplicateIDError{File: file, Line: lineNo, ID: id, FirstLine: first}
		}
		firstLine[id] = lineNo
		entries = append(entries, Entry{ID: id, Reason: reason, File: file, Line: lineNo})
	}
	if err := scanner.Err(); err != nil {
		return nil, &AllowlistReadError{File: file, Err: err}
	}
	return entries, nil
}

// Check applies the allowlist to a govulncheck -format json stream. A finding
// is reachable when its first trace frame names a function, as in
// govulncheck's text mode; module- and package-level findings do not count.
func Check(entries []Entry, stream io.Reader) (Result, error) {
	summaries := make(map[string]string)
	reachable := make(map[string]Finding)
	var order []string
	dec := json.NewDecoder(stream)
	for {
		var msg message
		err := dec.Decode(&msg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Result{}, &StreamError{Err: err}
		}
		if msg.OSV != nil {
			summaries[msg.OSV.ID] = msg.OSV.Summary
		}
		f := msg.Finding
		if f == nil || len(f.Trace) == 0 || f.Trace[0].Function == "" {
			continue
		}
		if _, seen := reachable[f.OSV]; seen {
			continue
		}
		reachable[f.OSV] = newFinding(f)
		order = append(order, f.OSV)
	}

	listed := make(map[string]Entry, len(entries))
	for _, e := range entries {
		listed[e.ID] = e
	}
	var result Result
	for _, id := range order {
		if e, ok := listed[id]; ok {
			// An exception covers only an advisory with no fixed release.
			if fd := reachable[id]; fd.FixedVersion != noFixedVersion {
				fd.Summary = summaries[id]
				result.Fixable = append(result.Fixable, fd)
				continue
			}
			result.Allowed = append(result.Allowed, Allowed{ID: id, Reason: e.Reason})
			continue
		}
		fd := reachable[id]
		fd.Summary = summaries[id]
		result.Unlisted = append(result.Unlisted, fd)
	}
	for _, e := range entries {
		if _, ok := reachable[e.ID]; !ok {
			result.Stale = append(result.Stale, e)
		}
	}
	return result, nil
}

// noFixedVersion is the FixedVersion of a finding govulncheck reports no
// fixed release for.
const noFixedVersion = "N/A"

func newFinding(f *finding) Finding {
	top := f.Trace[0]
	fixed := f.FixedVersion
	if fixed == "" {
		fixed = noFixedVersion
	}
	pos := "unknown position"
	if top.Position != nil && top.Position.Filename != "" {
		pos = top.Position.Filename + ":" + strconv.Itoa(top.Position.Line)
	}
	return Finding{
		ID:           f.OSV,
		Module:       top.Module,
		Version:      top.Version,
		FixedVersion: fixed,
		Position:     pos,
	}
}

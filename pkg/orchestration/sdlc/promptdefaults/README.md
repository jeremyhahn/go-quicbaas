# The system tenant's prompt defaults

Every markdown file under `sdlc/` is one thing the SDLC module says: to the
worker driving an issue, or to the person watching that issue on the forge.
**These files are the system tenant's set.** They are embedded in the binary,
registered as the `sdlc` set's defaults, and are what a tenant's own prompts
are seeded from and inherit when the tenant has not overridden them.

Editing one changes what a worker is told. The rules:

- A file's name is the prompt's name; `worker_charter.md` is `worker_charter`.
- `${name}` is a hole the runner fills. Every hole must be declared in
  `../prompt_set.go`, and a hole declared required must stay.
- There are no conditionals and no loops. A clause that appears only
  sometimes is its own prompt, spliced in through a hole.
- **Some literals may not be removed.** `worker_charter.md` must keep
  `SDLC-STOP:` and every stop code, because `turn.go` reads exactly those out
  of a worker's reply; the specification declares them from that file's own
  constants and a save that drops one is refused by name.
- The final newline is not part of the prompt. Whitespace between assembled
  pieces belongs to the Go assembler, not to the file.
- A file no specification declares is dead; the package's tests refuse it.

`go test ./pkg/orchestration/sdlc/ -run Prompt` checks all of that, and the golden tests in
`testdata/prompts/` say whether an edit changed what a worker reads.

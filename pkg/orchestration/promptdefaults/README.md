# The system tenant's prompt defaults

Every markdown file under `workflow/` is one prompt the orchestration engine
sends an agent. **These files are the system tenant's set.** They are
embedded in the binary, registered as the `workflow` set's defaults, and are
what a tenant's own prompts are seeded from and inherit when the tenant has
not overridden them.

Editing one changes what agents are told. The rules:

- A file's name is the prompt's name; `worker_prompt.md` is `worker_prompt`.
- `${name}` is a hole the engine fills. Every hole must be declared in
  `../prompt_set.go`, and a hole declared required must stay.
- There are no conditionals and no loops. A clause that appears only
  sometimes is its own prompt, spliced in through a hole.
- The final newline is not part of the prompt. Whitespace between assembled
  pieces belongs to the Go assembler, not to the file.
- A file no specification declares is dead; the package's tests refuse it.

`go test ./pkg/orchestration/ -run Prompt` checks all of that, and the golden
tests in `testdata/prompts/` say whether an edit changed what an agent reads.

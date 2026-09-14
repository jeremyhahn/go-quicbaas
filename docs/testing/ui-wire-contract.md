# The UI wire contract

`pkg/uicontract` compares the TypeScript SDK's hand-written response types
against the Go structs the server serialises, and fails when the two disagree
about a field name.

Run it with `make test-uicontract`; it is untagged, so `make test` runs it in
every build pass.

## The defect class it exists for

Nothing in the build compared the two sides. The Go structs are mirrored by
hand in `sdk/typescript/src/types.ts`, and a mirror that drifts still
compiles:

- the browser reads `session.last_active`,
- TypeScript confirms the field is declared,
- at runtime the server sent `last_activity`,
- the component renders `undefined` as an em-dash,
- every layer reports success.

The only detector was a person noticing that a column was wrong. Three such
fields shipped that way (go-quicbaas#855, go-quicbaas#856).

## Why the Go structs are the source

They are what `encoding/json` writes onto the wire. The TypeScript types are a
*claim* about that wire, and the frontend store's own interfaces are a second
claim about the first. A test driven from either TypeScript layer would have
passed through all three defects, because in every case the TypeScript
declared the field the UI read.

So the Go side is read by reflection — exact, not parsed — and the TypeScript
side is read as text.

## What it checks

For each binding (a TypeScript interface paired with a Go struct):

1. **Every field the interface declares is a field the struct writes.**
2. **A rename is allowed only if it is declared *and* performed.** The SDKs
   deliberately expose `SessionInfo.state` as `status` and
   `SessionInfo.last_activity` as `last_active`. The Go client does the rename
   in `SessionStatusWire.Response()` and the Wails binding does it in
   `OrchestratorBinding.ListSessions`. The REST client did neither, so the
   same Svelte component showed the time on the desktop and an em-dash in a
   browser tab. A declared rename with no mapping behind it now fails.
3. **`absent` entries are debts, not passes.** A field the server provably
   does not send may be recorded with the reason, and the test asserts it
   really is missing — so the entry must be deleted the moment the server
   starts sending it.

## What it cannot check

Name drift only. Two of the three defects in go-quicbaas#856 were *semantic*
drift: `spawned_job_id` and `assigned_to` both exist on the Go `Task` with
exactly those json tags, and both are legitimately empty on the records the
Tasks page lists —

- `spawned_job_id` is the **open** attempt, cleared by `recordAttemptHistory`
  the moment an attempt concludes,
- `assigned_to` is the **claimant**, which the dispatcher's own path never
  writes.

No name-comparing test can catch a field that exists and means something other
than what the reader assumed. Those are guarded by component tests that assert
what the column renders for a completed task
(`cmd/quicbaas/frontend/test/TasksView.test.ts`,
`.../TaskDetailView.test.ts`).

## Adding a binding

Add an entry to `bindings()` in `pkg/uicontract/wire_contract_test.go`. The
parse only reads flat interfaces and fails loudly on a nested one rather than
silently skipping fields, and `TestBindingsNameRealTypes` fails if a binding
names a type that no longer exists — a contract test that checks nothing is
worse than none, because it reports success.

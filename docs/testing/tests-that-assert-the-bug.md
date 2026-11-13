# Spotting a test that asserts the bug

A living list of heuristics, collected from tests found on this project's
board. It is not policy: these are signals that make a test worth a second
look, and they have a false-positive rate. Add an entry when a new mechanism
turns up.

## The variety that matters

Not a test that *cannot* fail, but a test that **faithfully asserts the bug**.
Coverage calls the line covered, the suite is green, and the assertion is a
correct description of what the code does. Examples found here:

| test | what it asserted |
|---|---|
| `TestP15_Initialize_IAMComponentsWarnPath` | initialize succeeds because an IAM error is non-fatal (a fail-open) |
| `TestRegisterSelf_FinishCreatesTenant` | finish surfaces a validation error after creating the tenant (self-registration never completed) |
| `TestGrantRoleIfMissing_Branches` | a role that does not exist can be granted |
| `TestCoreUsername_IsTheDisplayName` | the store's unique handle is the display name |
| the refresh suite | passed only because an unresolvable subject was inferred to be a service principal |
| `TestTimeoutFlagIsBoundOnce` (first draft) | `assert.Equal` on bound flag values, which cannot see identity |

## Signals

1. **The comment explains the behaviour instead of justifying it.** The
   strongest single signal. A test pinning a decision says why the decision
   is right; a test written around a defect narrates how the code happens to
   behave.
2. **The assertion is weaker than the test's name.** When the name promises
   an outcome and the body checks a precondition, ask what happened to the
   outcome.
3. **A fixture that would be invalid in production.** Members that are not
   users, a role granted without being created, a session naming no
   principal. Whatever the test proves is about a state the system never
   reaches. Asking *why* the fixture is shaped that way often yields a real
   finding.
4. **It passes under mutation.** The decisive check and the only one that is
   not a judgement call: break the code the test names and re-run it. When a
   test passes under mutation, suspect the fixture before the mutation (for
   example, an email local part equal to the handle being asserted, so the
   fallback produces the same string).
5. **It was added in the same change as the behaviour it pins.** Not damning
   alone, but a test added beside a workaround tends to describe the
   workaround. Run `git log -S` on the assertion when the behaviour looks odd.
6. **The comparator cannot see the difference the test exists to catch.**
   The right assertion on the right value, and still wrong. pflag stores the
   address of the bound variable in `Flag.Value`; testify's `Equal` follows
   `reflect.DeepEqual` through the pointer, so two distinct variables both
   holding 30s compare equal. Ask what the comparator can *distinguish*:
   where identity is the property (a pointer, a bound variable, a singleton,
   a shared instance), `assert.Same` or `==` is the assertion.
7. **The vacuous negative assertion.** `assert.NotContains(log, secret)`
   passes against an empty log. For every "X is not present" assertion
   (`NotContains`, `Empty`, `Nil`, "no error was logged"), ask what makes the
   *container* present, and assert that the path was taken first:

   ```go
   if !fellBack(log) {
       t.Fatal("the branch under test was not taken")
   }
   assert.NotContains(t, log, secret)
   ```

## What to do with one

Invert it; do not delete it. The name and fixture are usually reusable, and
the inverted test is the regression guard for the fix. Say in the new comment
what the test used to assert, so nobody restores the old behaviour and finds a
test that agrees with them.

Where a test used a legacy *call shape* while testing something else, update
the shape rather than deleting the test: it was testing live behaviour through
a dead door.

## The cost

Plenty of tests legitimately describe mechanisms, and plenty of fixtures are
simplified without being invalid. The cheap discriminator is mutation
(signal 4); spend time on it when the other signals merely make you
suspicious.

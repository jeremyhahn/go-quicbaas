# Composition — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/composition/`.

**No dedicated CLI surface, by design.** Composition is what the server does at
startup — building go-xkms hardware adapters and injecting them into go-qrdb —
not something an operator invokes. There is no `quicbaas compose` verb and there
should not be one: a composition decided at runtime by a command would be a
different backend than the one the server booted with.

What an operator *does* control is the configuration that composition reads, and
the flags that select it at boot:

| Where | What it decides |
|---|---|
| `quicbaasd start --fips` | FIPS 140-3 mode for the whole composed stack |
| `quicbaasd start --data-dir` | Where the composed stack persists |
| the `crypto:` config block | Which backend each crypto site resolves to |
| the `xkms:` config block | The hardware/cloud KMS adapters built at startup |

`engineering.md` is the reference for those keys. `quicbaasd`'s own flags are
documented in [`../multiprotocol-api/cli.md`](../multiprotocol-api/cli.md).

Two things worth knowing at the command line:

- **`--transport embedded`** on `quicbaas` runs the composed stack in-process
  rather than dialling a server. That is the composition surface reachable from
  the CLI, and it is a transport selection, not a composition command. See
  [user.md](user.md).
- **No subsystem calls a go-xkms constructor directly.** If you find yourself
  wanting a CLI verb to swap a backend at runtime, the answer is a config change
  and a restart — the injection points run once, at startup.

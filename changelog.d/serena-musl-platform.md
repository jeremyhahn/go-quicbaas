### Fixed

- **Serena could not read Go in the worker image, so every review lost symbol
  and reference lookup.** solidlsp composes its platform id as
  `<os>-<libc>-<arch>` whenever the libc is not glibc, and reads the libc from
  Python's `platform.libc_ver()` — which only ever detects glibc. On this musl
  image it returns `""`, producing `linux--x64`, which is not a member of the
  `PlatformId` enum and raises. `linux-musl-x64` is a member; nothing was
  missing but the detection.

  It broke Go even though nothing about Go asks for a platform: resolving any
  language server walks every `LanguageServerId` and *imports* every
  language-server module to compare classes, and the TypeScript module
  evaluates the platform id at import time. So starting the **Go** server
  imported the TypeScript module and died there. The reviewer then reported it
  could not complete the review and returned a verdict that decides nothing.

- **The image gate probed a handshake, not a lookup**, which is why it stayed
  green throughout. It started serena against a project root holding no code,
  so no language server was ever resolved — and resolving one was exactly
  where the failure lay. The gate now builds a Go module and resolves a symbol
  in it.

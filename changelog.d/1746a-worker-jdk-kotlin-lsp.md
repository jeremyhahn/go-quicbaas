### Added

- The Software Factory worker image carries OpenJDK 21 (Debian's
  `openjdk-21-jdk-headless`, pinned as `OPENJDK_MAJOR`) and JetBrains' Kotlin
  language server 263.6379.0 (`KOTLIN_LSP_VERSION`, SHA-256 pinned per
  architecture) as `kotlin-lsp` on the PATH, both recorded in the image's
  bill-of-materials labels and checked by the toolchain probe (#1746).
- A worker image probe starts `kotlin-lsp --stdio` offline, from an empty
  HOME, as the worker uid, and requires it to answer an LSP `initialize` with
  its capabilities and then a `shutdown`, all within one deadline (#1746).

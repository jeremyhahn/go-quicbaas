### Fixed

- The embedded transport now routes all eight `quicbaas.ParameterStore` verbs
  (`GetParameter`, `SetParameter`, `DeleteParameter`, `GetParameters`,
  `SetParameters`, `DeleteParameters`, `ListParameters`, `SearchByTags`), so
  the SDK parameter store client no longer fails with `UnknownMethodError`
  in-process (#1553).

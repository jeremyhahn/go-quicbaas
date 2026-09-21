### Fixed

- **A triage reply the binary cannot read is no longer posted as a verdict.**
  An undeclared or unreadable reply, a verdict carrying no evidence, and a
  duplicate that names the issue itself or an issue the pass never listed are
  now set aside: audited as `sdlc:triage_unusable`, written nowhere on the
  forge, and judged again on the next pass. Before, each was posted as
  "triage declared a verdict this binary cannot act on" with the escalated
  label — and that comment then became triage's last word, so the issue was
  frozen until a person spoke. Measured on go-luks2 on the NPU (2026-09-21):
  one pass posted a self-referencing duplicate with a misspelt marker, two
  27-byte not-actionables with no evidence, and a duplicate of an unrelated
  issue. Those are a small model running out of rounds, not findings about
  the work.

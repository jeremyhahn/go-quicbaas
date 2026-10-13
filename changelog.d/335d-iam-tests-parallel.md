### Changed

- **`pkg/iam`'s client-secret tests run in parallel (go-quicbaas#335).** Each
  of them derives PBKDF2-SHA256 at the production 600,000 iterations, and
  they ran one at a time. The ones that share no mutable state now call
  `t.Parallel()`. Under `-race`, the package dropped from about 18 minutes to
  under 6 on a 6-CPU runner, and the gain grows with the core count. The
  tests that swap a package seam (`verifySecret`, `tokenIDEntropy`) or
  compare wall-clock times stay sequential. The work factor and the
  30-minute timeout are unchanged.

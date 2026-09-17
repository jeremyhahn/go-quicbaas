### Changed

- **The reviewer charter no longer tells the reviewer to withhold a verdict.**
  It defined `comment` as "deciding it needs something you do not have" and
  closed with "if you cannot reach a verdict, say why and declare `comment`".
  A thorough reviewer can nearly always name something it could not confirm,
  so that made `comment` the default outcome — and a comment satisfies no
  gate, so the change waits forever and every later round is blocked too.
  Observed three times on clean changes: on go-luks2 #196 the reviewer read
  all 21 files and found no defect, and on #200 it traced every caller with
  Serena, found "no actionable code defects", and declared `comment` because a
  changelog fragment referenced a companion issue in another repository that
  it could not open.

  `comment` is now reserved for a change that could not be judged at all — an
  explicit draft, or a diff the reviewer could not read. An unverifiable
  detail is recorded as a caveat inside an approval, or, when the change
  *asserts* the thing that cannot be confirmed, as an actionable
  `request-changes` naming what to remove or prove. The rigour is unchanged:
  the reviewer is not told to approve anything it thinks is wrong, and the
  gate still requires an independent approval over green checks.

### Fixed

- **A commit status was treated as proof that a head had been reviewed.** The
  review floor decided "has this head been judged" from the presence of a
  status under the reviewer's context. A status is only a *projection* of a
  verdict, and the two disagree whenever a review declines to judge — the
  reviewer publishes a status, records no verdict, and the head then reads as
  reviewed forever while the gate correctly still wants a review for it.
  Nothing could ever dispatch one. Three finished, green changes on go-luks2
  were stranded exactly that way.

  The floor now asks the forge for the verdict, using the same
  `forgeReviewVerdicts` map the gate reduces reviews with, so "judged" means
  the same thing in both places. The two questions are separated: a *pending*
  status means a reviewer is at work and a second must not be started; whether
  the head was *judged* is answered by an approval or a request for changes
  bound to that head. A comment is neither.

  It costs one review listing per open pull request, paid before the answer is
  known. That is a deliberate trade: one forge read against a change that can
  never merge.

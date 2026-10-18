### Fixed

- KV Put, Get and Delete now refuse a cancelled or expired context before any read or write, on the embedded transport and in the service layer, returning an error that matches `context.Canceled` or `context.DeadlineExceeded`; a cancelled Put no longer commits and a cancelled Delete no longer removes the entity (#1583).

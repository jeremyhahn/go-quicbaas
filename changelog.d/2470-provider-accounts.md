### Added

- Default provider accounts: every tenant gets one default account for each frontier model provider (Anthropic and OpenAI), created at install and for every tenant created later, with the status `awaiting_credential`. Setting its credential at first login (the account credential rotate call) stores it in the tenant's secret store, never shows it again, and makes the account active; first login may instead point the account at a tenant secret (account update with a credential secret reference), which makes it active once the secret resolves (#2470).

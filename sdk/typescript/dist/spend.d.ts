/**
 * SpendLimitWindow is one sliding allowance: [now - size, now). At least one
 * of max_tokens and max_equivalent_micros must be positive; an absent or zero
 * one leaves that axis unmetered. Mirrors orchestration.QuotaWindow.
 */
export interface SpendLimitWindow {
    /** name identifies the window in refusals ("five-hour", "weekly"). */
    name: string;
    /** size is the window's length in NANOSECONDS (a Go time.Duration). */
    size: number;
    /** max_tokens caps every token the provider metered. */
    max_tokens?: number;
    /**
     * max_equivalent_micros caps the list-price equivalent, in millionths of a
     * US dollar, of the window's priced turns.
     */
    max_equivalent_micros?: number;
}
/**
 * SpendLimits is the one limits shape an account, a project and a tenant
 * carry (go-quicbaas#1058): an ordered set of windows, every one binding where enforced.
 * No windows meters nothing.
 */
export interface SpendLimits {
    windows?: SpendLimitWindow[];
}
/** SpendBreakerPolicy takes an account out of service after repeated failures. */
export interface SpendBreakerPolicy {
    /** window, failure_threshold failures within it trip the breaker (nanoseconds). */
    window: number;
    failure_threshold: number;
    /** cooldown is how long a tripped account stays out of service (nanoseconds). */
    cooldown: number;
}
/** AccountSpendPolicy is what an account may spend and how failures take it out of service. */
export interface AccountSpendPolicy {
    quota?: SpendLimits;
    breaker?: SpendBreakerPolicy;
}
//# sourceMappingURL=spend.d.ts.map
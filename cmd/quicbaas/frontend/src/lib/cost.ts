/**
 * cost.ts — the rules the Cost views render by.
 *
 * THE ONE RULE THIS FILE EXISTS FOR: a number the system does not have is
 * ABSENT AND LABELLED, never a zero. go-quicbaas#794 was filed because the
 * Cost page showed "0.0% Iterations" and "0/10" for jobs whose runner does not
 * iterate — figures that are structurally always zero, rendered as if they
 * were measurements. Three separate absences have to stay distinguishable:
 *
 *   - NO ITERATION BUDGET. Only the "workflow" runner iterates. A job driven
 *     by any other runner has no such budget, and a gauge reading 0% is a
 *     claim about a budget that does not exist.
 *   - NO SPEND POLICY. An account nobody configured a cap for is UNMETERED.
 *     That is a fact, and it is not "0% of budget used"; it is also not the
 *     same as 'unknown', which is what the server says when it could not read
 *     the account at all.
 *   - NO PRICE. A turn on a model the operator's price table does not name is
 *     UNPRICED, not free. A scope with no priced turn renders its money as a
 *     dash; a scope with some unpriced turns renders a floor and says so
 *     (go-quicbaas#791, #1058). The billed figure follows the same rule: an
 *     account whose billing nobody configured bills unknown, not zero.
 *
 * Pure functions only: no stores, no fetch. Everything here is directly
 * testable, which is what keeps the rules from being re-derived inline in a
 * Svelte template where nothing checks them.
 */

import type {
  CostSettings,
  ElectricityRateSource,
  GetSpendReportResponse,
  SetCostSettingsRequest,
  SpendAccount,
  SpendAccountBilling,
  SpendAgent,
  SpendChild,
  SpendHost,
  SpendLevel,
  SpendTotals,
  SpendWindowStatus,
} from '@quicbaas/sdk';
import { SPEND_UNATTRIBUTED } from '@quicbaas/sdk';
import { httpStatus, errorText } from './tasks.ts';

// UNMEASURED is what a figure the system does not have renders as. An em
// dash, not '0' and not 'N/A': it reads as "nothing here" to a person and is
// never mistaken for a measurement.
export const UNMEASURED = '—';

// SPEND_UNATTRIBUTED_LEVEL re-exports the ledger's no-value sentinel under a
// name that says what it is FOR at a call site: the value a scope supplies
// for a chain level the record has no value at. Re-exported here rather than
// imported from the SDK at every call site so the meaning travels with the
// rules that use it.
export const SPEND_UNATTRIBUTED_LEVEL = SPEND_UNATTRIBUTED;

/**
 * noIterationBudgetNotice is what a non-iterating job's panel says in place of
 * a gauge. It names the runner, because "this runner does not iterate" is only
 * actionable if the reader knows which runner ran.
 *
 * WHICH runner iterates is NOT decided here. `isWorkflowJob` in
 * lib/jobDetail.ts already owns that question for the whole job page, and a
 * second copy of the rule is the copy that goes stale. This file only supplies
 * the sentence for the case that one answers no.
 */
export function noIterationBudgetNotice(runner: string | undefined | null): string {
  const name = (runner ?? '').trim();
  return name === ''
    ? 'This job has no iteration budget.'
    : `The ${name} runner does not iterate, so this job has no iteration budget.`;
}

/**
 * formatTokens renders a token count for a person. A count the report does
 * not carry is UNMEASURED, so an absent value is a dash and a real zero is
 * still "0" — a scope that genuinely spent nothing is a measurement.
 */
export function formatTokens(tokens: number | undefined | null): string {
  if (typeof tokens !== 'number' || !Number.isFinite(tokens)) return UNMEASURED;
  return tokens.toLocaleString();
}

/** MICROS_PER_UNIT converts the ledger's integer micro-units to currency units. */
const MICROS_PER_UNIT = 1_000_000;

/**
 * formatMoney renders an integer amount of micro-units as currency. Amounts
 * below one cent that are not zero render as "<$0.01", so a local model priced
 * at a fraction of a cent never reads as free.
 */
export function formatMoney(micros: number, currency = 'USD'): string {
  if (!Number.isFinite(micros)) return UNMEASURED;
  if (micros > 0 && micros < MICROS_PER_UNIT / 100) {
    return `<${formatMoney(MICROS_PER_UNIT / 100, currency)}`;
  }
  return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(micros / MICROS_PER_UNIT);
}

type Priced = Pick<SpendTotals, 'equivalent_micros' | 'priced_entries' | 'unpriced_entries'>;
type Billed = Pick<SpendTotals, 'billed_micros' | 'billed_entries' | 'unbilled_entries'>;

/**
 * formatEquivalent renders the list-price equivalent — what the tokens would
 * have cost on the provider's metered API — ONLY when at least one turn in
 * the scope was priced. Otherwise it is a dash: a rendered 0 would claim the
 * work was free.
 */
export function formatEquivalent(totals: Priced | null | undefined, currency?: string): string {
  if (!totals || (totals.priced_entries ?? 0) === 0) return UNMEASURED;
  return formatMoney(totals.equivalent_micros ?? 0, currency);
}

/**
 * formatBilled renders what the scope added to the bill actually paid, under
 * the same rule: no turn with a known bill is a dash.
 */
export function formatBilled(totals: Billed | null | undefined, currency?: string): string {
  if (!totals || (totals.billed_entries ?? 0) === 0) return UNMEASURED;
  return formatMoney(totals.billed_micros ?? 0, currency);
}

/**
 * equivalentNote says how complete the equivalent is: unpriced entirely, a
 * floor over some unpriced turns, or complete.
 */
export function equivalentNote(totals: Priced | null | undefined): string {
  const priced = totals?.priced_entries ?? 0;
  const unpriced = totals?.unpriced_entries ?? 0;
  if (priced === 0) return unpriced > 0 ? 'no price for these models' : 'nothing priced';
  if (unpriced > 0) return `at least — ${unpriced.toLocaleString()} turn${unpriced === 1 ? '' : 's'} unpriced`;
  return 'at list price';
}

/**
 * billedNote says how complete the billed figure is, in the same terms.
 */
export function billedNote(totals: Billed | null | undefined): string {
  const billed = totals?.billed_entries ?? 0;
  const unbilled = totals?.unbilled_entries ?? 0;
  if (billed === 0) return unbilled > 0 ? 'billing not configured' : 'nothing billed';
  if (unbilled > 0) return `at least — ${unbilled.toLocaleString()} turn${unbilled === 1 ? '' : 's'} with unknown billing`;
  return 'actually billed';
}

/**
 * formatEquivalentFloor is formatEquivalent for a breakdown row, where there
 * is no room for equivalentNote: a figure that covers only some of the row's
 * turns is marked "≥", because it is a floor, not the row's cost.
 */
export function formatEquivalentFloor(totals: Priced | null | undefined, currency?: string): string {
  const figure = formatEquivalent(totals, currency);
  if (figure === UNMEASURED || (totals?.unpriced_entries ?? 0) === 0) return figure;
  return `≥${figure}`;
}

/**
 * formatBilledFloor is formatBilled for a breakdown row, under the same rule:
 * a billed figure over some turns of unknown billing is marked "≥".
 */
export function formatBilledFloor(totals: Billed | null | undefined, currency?: string): string {
  const figure = formatBilled(totals, currency);
  if (figure === UNMEASURED || (totals?.unbilled_entries ?? 0) === 0) return figure;
  return `≥${figure}`;
}

/**
 * moneyNote is both coverage notes on one line — the tooltip a breakdown row
 * carries so its "≥" and dashes explain themselves.
 */
export function moneyNote(totals: (Priced & Billed) | null | undefined): string {
  return `equivalent: ${equivalentNote(totals)} · billed: ${billedNote(totals)}`;
}

type Energy = Pick<SpendTotals, 'energy_mwh' | 'electricity_entries' | 'measured_energy_entries'>;

/**
 * formatEnergy renders the energy local inference drew, from milliwatt-hours:
 * Wh below a kilowatt-hour, kWh above. A scope with no turn costed from
 * electricity is a dash — no measurement, not zero energy.
 */
export function formatEnergy(totals: Energy | null | undefined): string {
  if (!totals || (totals.electricity_entries ?? 0) === 0) return UNMEASURED;
  const milliWh = totals.energy_mwh ?? 0;
  if (milliWh >= 1_000_000) {
    return `${(milliWh / 1_000_000).toLocaleString('en-US', { maximumFractionDigits: 2 })} kWh`;
  }
  if (milliWh > 0 && milliWh < 10) return '<0.01 Wh';
  return `${(milliWh / 1000).toLocaleString('en-US', { maximumFractionDigits: 2 })} Wh`;
}

/**
 * energyNote says how many turns the energy covers, and how many of them were
 * MEASURED — costed from the fleet's device telemetry — rather than estimated
 * from a configured draw. The split is the honest part: an estimated watt-hour
 * reads the same as a measured one unless something says which it is.
 */
export function energyNote(totals: Energy | null | undefined): string {
  const turns = totals?.electricity_entries ?? 0;
  if (turns === 0) return 'no turn costed from electricity';
  return `${turns.toLocaleString()} local turn${turns === 1 ? '' : 's'} costed from electricity · ${measuredShare(totals)}`;
}

/**
 * measuredShare says how much of a scope's energy was measured: "all
 * measured", "all estimated", or "3 of 5 measured".
 */
export function measuredShare(totals: Energy | null | undefined): string {
  const turns = totals?.electricity_entries ?? 0;
  const measured = Math.min(totals?.measured_energy_entries ?? 0, turns);
  if (turns === 0) return UNMEASURED;
  if (measured === turns) return 'all measured';
  if (measured === 0) return 'all estimated';
  return `${measured.toLocaleString()} of ${turns.toLocaleString()} measured`;
}

/**
 * hostLabel names a host row. Turns that named no host are an absence, and
 * read as one rather than as a host called "-".
 */
export function hostLabel(host: SpendHost): string {
  return host.unattributed === true ? 'No host recorded' : (host.host ?? '');
}

/**
 * hostScopeValue is what narrowing the report to a host row supplies: the
 * host, or the ledger's no-value sentinel for the turns that named none.
 */
export function hostScopeValue(host: SpendHost): string {
  return host.unattributed === true ? SPEND_UNATTRIBUTED : (host.host ?? '');
}

/** hostFilterLabel names the host a report is narrowed to, for its chip. */
export function hostFilterLabel(value: string): string {
  return value === SPEND_UNATTRIBUTED ? 'No host recorded' : value;
}

/**
 * agentLabel names an Agent row: the Agent's name, else its ID. The turns of
 * raw-backend sessions are an absence and read as one.
 */
export function agentLabel(agent: SpendAgent): string {
  if (agent.unattributed === true) return 'No Agent (raw backend)';
  return agent.name || agent.agent_id || '';
}

/**
 * agentScopeValue is what narrowing the report to an Agent row supplies: the
 * Agent's ID, or the ledger's no-value sentinel for raw-backend sessions.
 */
export function agentScopeValue(agent: SpendAgent): string {
  return agent.unattributed === true ? SPEND_UNATTRIBUTED : (agent.agent_id ?? '');
}

/** agentFilterLabel names the Agent a report is narrowed to, for its chip. */
export function agentFilterLabel(value: string): string {
  return value === SPEND_UNATTRIBUTED ? 'No Agent (raw backend)' : value;
}

/**
 * electricitySourceLabel is how the source of the electricity rate in force
 * reads.
 */
export function electricitySourceLabel(source: ElectricityRateSource | string | undefined): string {
  switch (source) {
    case 'tenant_settings':
      return 'set for this tenant';
    case 'tenant_config':
      return "from the tenant's configuration";
    case 'deployment_config':
      return "from the deployment's configuration";
    case 'default':
      return 'the default';
    default:
      return 'unknown source';
  }
}

/** CostSettingsForm is the editable state of the cost settings control. */
export type CostSettingsForm = {
  // price is the $/kWh as typed; blank inherits the configuration.
  price: string;
  engines: { engine: string; watts: string }[];
};

/**
 * costSettingsForm is the form for a tenant's settings as they stand: only
 * what the tenant set, so saving it unchanged changes nothing.
 */
export function costSettingsForm(settings: CostSettings | null | undefined): CostSettingsForm {
  const set = settings?.settings;
  const engines = Object.entries(set?.engine_power_watts ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([engine, watts]) => ({ engine, watts: String(watts) }));
  return {
    price: set?.electricity_price_per_kwh === undefined ? '' : String(set.electricity_price_per_kwh),
    engines,
  };
}

/**
 * costSettingsRequest turns the form into the replacing request, or explains
 * why it cannot: the server refuses the same things, and saying so before the
 * round trip keeps a typo from silently becoming "inherit".
 */
export function costSettingsRequest(
  tenantID: string,
  form: CostSettingsForm,
): { request: SetCostSettingsRequest } | { error: string } {
  const request: SetCostSettingsRequest = { tenant_id: tenantID };
  const price = form.price.trim();
  if (price !== '') {
    const rate = Number(price);
    if (!Number.isFinite(rate) || rate < 0) return { error: 'The electricity price must be a non-negative number of dollars per kWh.' };
    request.electricity_price_per_kwh = rate;
  }
  const seen = new Set<string>();
  for (const row of form.engines) {
    const engine = row.engine.trim();
    const wattsText = row.watts.trim();
    if (engine === '' && wattsText === '') continue;
    if (engine === '') return { error: 'Every engine draw needs an engine name.' };
    if (!/^[A-Za-z0-9._-]+$/.test(engine)) return { error: `"${engine}" is not an engine name.` };
    if (seen.has(engine)) return { error: `"${engine}" is listed twice.` };
    const watts = Number(wattsText);
    if (wattsText === '' || !Number.isFinite(watts) || watts < 0) {
      return { error: `The draw for "${engine}" must be a non-negative number of watts.` };
    }
    seen.add(engine);
    request.engine_power_watts = { ...(request.engine_power_watts ?? {}), [engine]: watts };
  }
  return { request };
}

/**
 * billingLabel is how an account's billing reads: its mode and, for a
 * subscription, its monthly fee.
 */
export function billingLabel(billing: SpendAccountBilling | null | undefined, currency?: string): string {
  const mode = (billing?.mode ?? '').trim();
  if (mode === '') return 'Billing not configured';
  if (mode === 'subscription') {
    const fee = billing?.monthly_fee_micros ?? 0;
    return fee > 0 ? `Subscription · ${formatMoney(fee, currency)}/mo` : 'Subscription';
  }
  if (mode === 'api') return 'Metered API';
  return mode;
}

/**
 * formatFeeShare renders the part of a subscription's monthly fee that falls
 * in a report's range (go-quicbaas#1058, ruling F4). The server sends it only
 * for a tenant-level report and only for a subscription whose fee is
 * configured, so an absent figure is a dash, never a zero.
 */
export function formatFeeShare(micros: number | undefined | null, currency?: string): string {
  if (typeof micros !== 'number') return UNMEASURED;
  return formatMoney(micros, currency);
}

/**
 * SpendRange is how far back the Cost page reads: every turn the ledger holds,
 * or the last day, week or thirty days.
 */
export type SpendRange = 'all' | '24h' | '7d' | '30d';

/** SPEND_RANGES lists the choices in the order the page offers them. */
export const SPEND_RANGES: { value: SpendRange; label: string }[] = [
  { value: 'all', label: 'All time' },
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
];

const SPEND_RANGE_MILLIS: Record<Exclude<SpendRange, 'all'>, number> = {
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
};

/**
 * spendRangeFrom is the RFC 3339 start of a range ending now, or undefined for
 * every turn. Only the start is sent: the server closes a range at its own now,
 * so a client clock that is off does not move the end.
 */
export function spendRangeFrom(range: SpendRange, now: Date): string | undefined {
  if (range === 'all') return undefined;
  return new Date(now.getTime() - SPEND_RANGE_MILLIS[range]).toISOString();
}

/**
 * policyLabel is what an account's spend policy reads as.
 *
 * The three answers are deliberately distinct sentences. Collapsing 'unknown'
 * into 'No spend policy' would let a deployment that wires no account store
 * report every account as uncapped.
 */
export function policyLabel(account: Pick<SpendAccount, 'policy_status'>): string {
  switch (account.policy_status) {
    case 'metered':
      return 'Metered';
    case 'none':
      return 'No spend policy';
    default:
      return 'Spend policy unknown';
  }
}

/**
 * policyVariant maps a policy status onto a badge variant. Map dispatch, so a
 * status this file does not know about renders neutrally rather than as the
 * reassuring one.
 */
const POLICY_VARIANTS: Record<string, 'success' | 'info' | 'warning'> = {
  metered: 'success',
  none: 'info',
  unknown: 'warning',
};

export function policyVariant(account: Pick<SpendAccount, 'policy_status'>): 'success' | 'info' | 'warning' {
  return POLICY_VARIANTS[account.policy_status] ?? 'warning';
}

/**
 * windowPercent is how much of a quota window has been drawn, 0-100.
 *
 * A window with no allowance cannot be expressed as a percentage, and the
 * server refuses to store one; if one ever arrives, this returns null rather
 * than dividing by zero into Infinity or claiming 0%.
 */
export function windowPercent(
  window: Pick<SpendWindowStatus, 'max_tokens' | 'used_tokens' | 'max_equivalent_micros' | 'used_equivalent_micros'>,
): number | null {
  // The window is as full as its fullest metered axis: that is the one that
  // refuses first.
  const ratios: number[] = [];
  if (Number.isFinite(window.max_tokens) && window.max_tokens > 0) {
    ratios.push(window.used_tokens / window.max_tokens);
  }
  const maxMicros = window.max_equivalent_micros ?? 0;
  if (Number.isFinite(maxMicros) && maxMicros > 0) {
    ratios.push((window.used_equivalent_micros ?? 0) / maxMicros);
  }
  if (ratios.length === 0) return null;
  return Math.min(100, Math.max(...ratios) * 100);
}

/**
 * windowSummary renders one window as a sentence: what is spent, out of what,
 * over how long — on each axis the window meters.
 */
export function windowSummary(window: SpendWindowStatus): string {
  const parts: string[] = [];
  if (window.max_tokens > 0) {
    parts.push(`${formatTokens(window.used_tokens)} / ${formatTokens(window.max_tokens)} tokens`);
  }
  const maxMicros = window.max_equivalent_micros ?? 0;
  if (maxMicros > 0) {
    parts.push(`${formatMoney(window.used_equivalent_micros ?? 0)} / ${formatMoney(maxMicros)}`);
  }
  return `${parts.join(' · ')} per ${formatDurationNanos(window.size)}`;
}

/**
 * formatDurationNanos renders a Go time.Duration (nanoseconds on the wire) as
 * the coarsest unit that divides it evenly, so "5h" does not read as "300m".
 *
 * NOTHING IS ROUNDED AWAY. A quota window is any positive time.Duration the
 * policy validator accepted, and the wire carries nanoseconds so that a window
 * shorter than a second survives the trip. Rounding to whole seconds first
 * rendered a 100ms window as "0s" -- a real limit that reads as no limit -- and
 * a 1.5s window as "2s".
 */
export function formatDurationNanos(nanos: number | undefined | null): string {
  if (typeof nanos !== 'number' || !Number.isFinite(nanos) || nanos <= 0) return UNMEASURED;
  if (nanos < 1_000_000_000) return `${trimFloat(nanos / 1_000_000)}ms`;

  const seconds = nanos / 1_000_000_000;
  const units: [number, string][] = [
    [86_400, 'd'],
    [3_600, 'h'],
    [60, 'm'],
  ];
  for (const [size, suffix] of units) {
    if (seconds % size === 0 && seconds >= size) return `${seconds / size}${suffix}`;
  }
  return `${trimFloat(seconds)}s`;
}

/**
 * trimFloat renders a number without the trailing digits binary floating point
 * adds to an exact decimal, so 1.5 prints as "1.5" and not "1.5000000000000002".
 */
function trimFloat(value: number): string {
  return String(Number(value.toPrecision(12)));
}

/**
 * childLabel names a drill-down row.
 *
 * The ledger files spend with no parent at a level — a job started outside any
 * task — under a sentinel. It must read as an absence, never as an entity
 * whose identifier is a dash.
 *
 * A row the server could NAME reads as its name: the By Project card listed
 * bare project ids, which nobody reading the page can map back to a
 * repository (go-quicbaas#1058). A project that no longer exists keeps its
 * spend under its id, so the id is the fallback.
 */
export function childLabel(child: SpendChild): string {
  if (child.unattributed === true) return `No ${child.level}`;
  const name = (child.name ?? '').trim();
  return name !== '' ? name : (child.id ?? '');
}

/** childIsNamed says whether a row renders a name rather than an identifier. */
export function childIsNamed(child: SpendChild): boolean {
  return child.unattributed !== true && (child.name ?? '').trim() !== '';
}

/**
 * childScopeValue is what a drill step supplies for the child's level.
 *
 * An unattributed row is ADDRESSABLE even though it has no id: the ledger's
 * sentinel names that subtree, and the server refuses it as a real entity id,
 * so the two can never be confused. A row a reader can see is a row a reader
 * can open.
 */
export function childScopeValue(child: SpendChild): string {
  return child.unattributed === true ? SPEND_UNATTRIBUTED : (child.id ?? '');
}

/**
 * childRound is the round a round-level row addresses, or null when the row is
 * not one or addresses no round.
 *
 * A ROUND IS A NUMBER, NOT AN ID (go-quicbaas#886). The server un-pads the
 * ledger's round segment to its decimal, and a segment it could not read comes
 * through as stored; only a non-negative integer is a round the server will
 * accept, so anything else — and the unattributed bucket, which no number can
 * address — is not a round a drill step can open.
 */
export function childRound(child: SpendChild): number | null {
  if (child.level !== 'round' || child.unattributed === true) return null;
  const id = child.id ?? '';
  if (!/^\d+$/.test(id)) return null;
  const round = Number(id);
  return Number.isSafeInteger(round) ? round : null;
}

/** childIsDrillable says whether a row addresses something that can be opened. */
export function childIsDrillable(child: SpendChild): boolean {
  if (child.level === 'round') return childRound(child) !== null;
  return childScopeValue(child) !== '';
}

/** roundLabel names a round for the breadcrumb: a bare number reads as nothing. */
export function roundLabel(round: number): string {
  return `${levelLabel('round')} ${round}`;
}

/**
 * scopeLabel renders a chain level's value for a person, mapping the ledger's
 * no-value sentinel to the absence it stands for. Printing the raw '-' as a
 * project name is the exact lie this file exists to prevent.
 */
export function scopeLabel(level: SpendLevel | string, value: string, name = ''): string {
  if (value === SPEND_UNATTRIBUTED) return `No ${level}`;
  const trimmed = name.trim();
  return trimmed !== '' ? trimmed : value;
}

/**
 * LEVEL_LABELS names each chain level for a person. Map dispatch rather than
 * a switch, and a level this build does not know renders as itself rather
 * than as a blank.
 */
const LEVEL_LABELS: Record<string, string> = {
  tenant: 'Tenant',
  project: 'Project',
  task: 'Task',
  job: 'Job',
  session: 'Session',
  round: 'Round',
};

export function levelLabel(level: SpendLevel | string | undefined | null): string {
  const name = (level ?? '').trim();
  return LEVEL_LABELS[name] ?? name;
}

/**
 * spendErrorMessage turns a failed spend read into a sentence that says what
 * the reader can do about it.
 *
 * IT ADDS THE ONE STATUS lib/tasks.ts HAS NO OPINION ABOUT: 503, which the
 * server returns when the deployment records no consumption at all. That is
 * the opposite of "nothing has been spent", and rendering it as an empty
 * report is precisely the reassuring lie this feature exists to remove.
 */
export function spendErrorMessage(err: unknown, subject: string): string {
  if (httpStatus(err) === 503) {
    return `This deployment is not recording provider consumption, so ${subject} cannot be reported.`;
  }
  switch (httpStatus(err)) {
    case 401:
      return `Your session is no longer valid. Sign in again to view ${subject}.`;
    case 403:
      return `You do not have permission to view ${subject}: this account lacks orchestrator:status.`;
    case 400:
      return `Could not read ${subject}: ${errorText(err)}`;
    default:
      return `Could not load ${subject}: ${errorText(err)}`;
  }
}

/**
 * reportProjectName is the breadcrumb name an accepted spend report carries for
 * the project in scope: the report's own `project_name`, or empty when it has
 * none. Every accepted report replaces the previous name, so a report that
 * carries no name (a tenant-level scope, another tenant, a deleted project)
 * clears it rather than leaving the last project's name on screen.
 */
export function reportProjectName(report: Pick<GetSpendReportResponse, 'project_name'>): string {
  return report.project_name ?? '';
}

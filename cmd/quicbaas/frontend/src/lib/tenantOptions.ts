/**
 * Tenant selector options.
 *
 * A tenant listing (client.iam.listTenants) is a discovery call for principals
 * permitted to see other tenants, not the definition of what a selector
 * offers: a tenant-scoped operator is (correctly) refused it. The session's own
 * tenant is therefore always present and selectable, and the listing only ever
 * ADDS entries (go-quicbaas#309, #312).
 */

/** A tenant a selector offers: its id and the label shown for it. */
export interface TenantOption {
  id: string;
  name: string;
}

/**
 * tenantOptionsOf turns a tenant listing into selector options: repeated ids
 * are dropped (the first wins) and a tenant without a name is labelled by id.
 */
export function tenantOptionsOf(listing: Array<{ id: string; name?: string }>): TenantOption[] {
  const options: TenantOption[] = [];
  for (const t of listing) {
    if (!options.some((o) => o.id === t.id)) {
      options.push({ id: t.id, name: t.name || t.id });
    }
  }
  return options;
}

/**
 * withRequiredTenants returns `listed` plus every non-empty id in `required`
 * that the listing lacks. Missing ids are prepended in order, so the last id
 * given heads the list. Every id a <select> may hold must be passed, or the
 * control renders a value it has no option for and shows blank.
 */
export function withRequiredTenants(listed: TenantOption[], required: string[]): TenantOption[] {
  const options = [...listed];
  for (const id of required) {
    if (id !== '' && !options.some((t) => t.id === id)) {
      options.unshift({ id, name: id });
    }
  }
  return options;
}

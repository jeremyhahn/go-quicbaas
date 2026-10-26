// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

/**
 * @quicbaas/svelte-security
 *
 * Reusable Svelte 5 components for the QuicBaaS Barrier / Ceremony /
 * Bootstrap security surfaces. Each component is purely presentational and
 * receives an injected client (BarrierClient / CeremonyClient) via prop so
 * the same component can be reused in both desktop (Wails) and web targets.
 */

export { default as BarrierStatusBadge } from './components/BarrierStatusBadge.svelte';
export { default as BarrierUnsealForm } from './components/BarrierUnsealForm.svelte';
export { default as ShamirShareInput } from './components/ShamirShareInput.svelte';
export { default as BarrierStrategyPicker } from './components/BarrierStrategyPicker.svelte';
export { default as CeremonyOfficerList } from './components/CeremonyOfficerList.svelte';
export { default as CeremonyClaimChallenge } from './components/CeremonyClaimChallenge.svelte';
export { default as BootstrapMethodPicker } from './components/BootstrapMethodPicker.svelte';

export type {
  BarrierStrategy,
  BarrierStatus,
  BarrierInitRequest,
  BarrierInitResponse,
  BarrierUnsealRequest,
  BarrierUnsealResponse,
  BarrierQuorumBeginRequest,
  BarrierQuorumChallenge,
  BarrierQuorumSubmitRequest,
  BarrierQuorumProgress,
  BarrierClient,
  TenantBarrierStrategy,
  TenantBarrierInitParams,
  BarrierHolderParams,
  OfficerKind,
  OfficerClaimStatus,
  CeremonyOfficer,
  ShareClaimChallenge,
  ClaimShareRequest,
  ClaimedShare,
  ShareClaimClient,
  EnrollOfficerRequest,
  EnrolledOfficer,
  CeremonyClient,
  BootstrapMethod,
  BootstrapRequest,
  BootstrapResponse,
} from './types.ts';

export {
  MTLS_CHALLENGE_CONTEXT,
  METHOD_DATA_PURPOSE,
  ChallengeMessageError,
  quorumChallengePurpose,
  quorumChallengeMessage,
  SHARE_CLAIM_CONTEXT,
  SYSTEM_CLAIM_LABEL,
  tenantClaimLabel,
  shareClaimMessage,
} from './challengeMessage.ts';

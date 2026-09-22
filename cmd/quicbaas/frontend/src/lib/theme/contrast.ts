// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
 * WCAG 2 relative-luminance contrast, computed directly from sRGB hex — the
 * same arithmetic the accessibility spec defines, and the same arithmetic a
 * browser's own contrast checker runs. There is no dependency on a design
 * tool or an external package: every token this app ships is a plain hex
 * string, and this is the entire formula needed to grade one against another.
 */

/** hexToRgb reads a 3- or 6-digit hex colour into its [r, g, b] byte triple. */
export function hexToRgb(hex: string): [number, number, number] {
  const bare = hex.replace('#', '');
  const full = bare.length === 3 ? bare.split('').map((c) => c + c).join('') : bare;
  const n = parseInt(full, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/** channelLuminance applies the sRGB piecewise gamma curve to one 0-255 channel. */
function channelLuminance(byte: number): number {
  const c = byte / 255;
  return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

/** relativeLuminance is the WCAG-defined perceived brightness of a colour, 0-1. */
export function relativeLuminance(hex: string): number {
  const [r, g, b]: readonly [number, number, number] = hexToRgb(hex).map(channelLuminance) as [
    number,
    number,
    number,
  ];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/**
 * contrastRatio is the WCAG 2 contrast ratio between two colours, from 1
 * (identical) to 21 (black on white). Order of the arguments does not
 * matter — the brighter one is always the numerator.
 */
export function contrastRatio(hexA: string, hexB: string): number {
  const a = relativeLuminance(hexA);
  const b = relativeLuminance(hexB);
  const hi = Math.max(a, b);
  const lo = Math.min(a, b);
  return (hi + 0.05) / (lo + 0.05);
}

/** AA_BODY is the minimum ratio WCAG AA requires for normal-sized text. */
export const AA_BODY = 4.5;

/** AA_LARGE is the minimum ratio WCAG AA requires for large text (18pt+/14pt bold) or UI chrome. */
export const AA_LARGE = 3.0;

/** meetsAA says whether a foreground/background pair clears the given AA floor. */
export function meetsAA(fg: string, bg: string, minimum: number = AA_BODY): boolean {
  return contrastRatio(fg, bg) >= minimum;
}

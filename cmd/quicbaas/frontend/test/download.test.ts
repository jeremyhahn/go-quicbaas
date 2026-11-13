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

import { afterEach, describe, expect, it, vi } from 'vitest';
import { downloadText } from '../src/lib/download.ts';

// readBlob reads a Blob as text through FileReader (jsdom's Blob has no text()).
function readBlob(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = () => reject(reader.error);
    reader.readAsText(blob);
  });
}

describe('downloadText', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  function stubDownload() {
    const blobs: Blob[] = [];
    const createObjectURL = vi.fn((b: Blob) => {
      blobs.push(b);
      return 'blob:download-1';
    });
    const revokeObjectURL = vi.fn();
    Object.assign(URL, { createObjectURL, revokeObjectURL });
    const clicks: HTMLAnchorElement[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      // The anchor is in the document while it is clicked.
      expect(document.body.contains(this)).toBe(true);
      clicks.push(this);
    });
    return { blobs, clicks, createObjectURL, revokeObjectURL };
  }

  it('saves the content under the file name as a PEM file by default', async () => {
    const { blobs, clicks, revokeObjectURL } = stubDownload();

    downloadText('ca.pem', '-----BEGIN CERTIFICATE-----\n');

    expect(clicks).toHaveLength(1);
    expect(clicks[0].download).toBe('ca.pem');
    expect(clicks[0].href).toBe('blob:download-1');
    expect(blobs).toHaveLength(1);
    expect(blobs[0].type).toBe('application/x-pem-file');
    expect(await readBlob(blobs[0])).toBe('-----BEGIN CERTIFICATE-----\n');
    // The anchor is removed and the object URL released.
    expect(document.body.contains(clicks[0])).toBe(false);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:download-1');
  });

  it('uses the MIME type the caller names', () => {
    const { blobs } = stubDownload();

    downloadText('bundle.json', '{}', 'application/json');

    expect(blobs[0].type).toBe('application/json');
  });
});

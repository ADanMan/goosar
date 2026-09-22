import { describe, expect, it, vi } from 'vitest';

import {
  daemonLifecycleUnreachable,
  isDaemonExternallyManaged,
  normalizeHostOS,
} from './daemon-os';

describe('normalizeHostOS', () => {
  it("maps win32 to the GOOS spelling 'windows'", () => {
    expect(normalizeHostOS('win32')).toBe('windows');
  });

  it('passes darwin and linux through unchanged (already GOOS spellings)', () => {
    expect(normalizeHostOS('darwin')).toBe('darwin');
    expect(normalizeHostOS('linux')).toBe('linux');
  });
});

describe('isDaemonExternallyManaged', () => {
  it('flags a Linux (WSL2) daemon behind a Windows desktop — the #3916 case', () => {
    expect(isDaemonExternallyManaged('linux', normalizeHostOS('win32'))).toBe(true);
  });

  it('does NOT flag a native Windows daemon under a Windows desktop', () => {
    expect(isDaemonExternallyManaged('windows', normalizeHostOS('win32'))).toBe(false);
  });

  it('does NOT flag a native macOS daemon under a macOS desktop', () => {
    expect(isDaemonExternallyManaged('darwin', normalizeHostOS('darwin'))).toBe(false);
  });

  it('does NOT flag a native Linux daemon under a Linux desktop', () => {
    expect(isDaemonExternallyManaged('linux', normalizeHostOS('linux'))).toBe(false);
  });

  it('fails safe to false when the daemon reports no OS', () => {
    expect(isDaemonExternallyManaged(undefined, 'windows')).toBe(false);
    expect(isDaemonExternallyManaged('', 'windows')).toBe(false);
  });
});

describe('daemonLifecycleUnreachable', () => {
  it('consults the live OS reader and blocks a foreign-OS (WSL2) daemon', async () => {
    const readDaemonOS = vi.fn().mockResolvedValue('linux');
    expect(await daemonLifecycleUnreachable(readDaemonOS, 'windows')).toBe(true);
    expect(readDaemonOS).toHaveBeenCalledTimes(1);
  });

  it('allows a native daemon whose live OS matches the host', async () => {
    expect(await daemonLifecycleUnreachable(async () => 'windows', 'windows')).toBe(false);
    expect(await daemonLifecycleUnreachable(async () => 'darwin', 'darwin')).toBe(false);
  });

  it('fails safe to false when the live OS is unknown (older daemon / none running)', async () => {
    expect(await daemonLifecycleUnreachable(async () => undefined, 'windows')).toBe(false);
  });
});

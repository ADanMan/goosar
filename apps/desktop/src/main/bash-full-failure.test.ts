import { describe, expect, it, vi } from 'vitest';

vi.mock('electron', () => ({
  app: {
    getAppPath: () => '/nonexistent-app-path',
    isPackaged: false,
    on: vi.fn(),
    quit: vi.fn(),
  },
  ipcMain: { handle: vi.fn(), on: vi.fn() },
  BrowserWindow: class {},
  shell: { openPath: vi.fn() },
  safeStorage: {
    isEncryptionAvailable: () => false,
    encryptString: vi.fn(),
    decryptString: vi.fn(),
  },
}));

import { describeBashFullFailure } from './daemon-manager';

describe('describeBashFullFailure (T-13, #697)', () => {
  it('names the actually-invalid block when it differs from security.bash_full', () => {
    const message =
      "mcp_servers.ews-mcp.stdio: Value error, mcp stdio server requires 'command' or 'docker_profile'";

    expect(describeBashFullFailure(message)).toBe(
      "config.user.yaml невалиден: mcp_servers.ews-mcp.stdio: Value error, mcp stdio server requires 'command' or 'docker_profile'",
    );
  });

  it('leaves a genuine security.bash_full failure message untouched', () => {
    const message = 'security.bash_full: Value error, must be a boolean';
    expect(describeBashFullFailure(message)).toBe(message);
  });

  it('leaves an unstructured message (no leading key:) untouched', () => {
    const message = 'hermes rejected that value, so it was not written.';
    expect(describeBashFullFailure(message)).toBe(message);
  });
});

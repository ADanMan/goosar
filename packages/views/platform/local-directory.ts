// Десктопные помощники для сценария local_directory ресурсов проекта.

export type PickDirectoryResult = {
  ok: boolean;
  path?: string;
  basename?: string;
  reason?: 'cancelled' | 'no_window' | 'error' | 'unsupported';
  error?: string;
};

export type ValidateLocalDirectoryResult = {
  ok: boolean;
  reason?:
    | 'not_absolute'
    | 'not_found'
    | 'not_a_directory'
    | 'not_readable'
    | 'not_writable'
    | 'error'
    | 'unsupported';
  error?: string;
};

interface DesktopLocalDirectoryAPI {
  pickDirectory?: (defaultPath?: string) => Promise<PickDirectoryResult>;
  validateLocalDirectory?: (path: string) => Promise<ValidateLocalDirectoryResult>;
}

function readDesktopAPI(): DesktopLocalDirectoryAPI | undefined {
  if (typeof window === 'undefined') return undefined;
  const api = (window as unknown as { desktopAPI?: DesktopLocalDirectoryAPI }).desktopAPI;
  return api;
}

export function isDesktopShell(): boolean {
  const api = readDesktopAPI();
  return typeof api?.pickDirectory === 'function';
}

export async function pickDirectory(defaultPath?: string): Promise<PickDirectoryResult> {
  const api = readDesktopAPI();
  if (!api?.pickDirectory) return { ok: false, reason: 'unsupported' };
  return api.pickDirectory(defaultPath);
}

export async function validateLocalDirectory(path: string): Promise<ValidateLocalDirectoryResult> {
  const api = readDesktopAPI();
  if (!api?.validateLocalDirectory) return { ok: false, reason: 'unsupported' };
  return api.validateLocalDirectory(path);
}

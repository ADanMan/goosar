// Чистый парсер deep-ссылок goosar://, вынесенный из главного процесса для
// тестирования без Electron. Callback авторизации несёт ровно одну из двух
// учётных данных: готовый token или одноразовый код.

export const DEEP_LINK_PROTOCOL = 'goosar';

export type DeepLinkAction =
  | { kind: 'auth-token'; token: string }
  | { kind: 'auth-link-token'; linkToken: string }
  | { kind: 'auth-mfa-token'; mfaToken: string }
  | { kind: 'auth-error'; code: string }
  | { kind: 'invite'; invitationId: string };

export function parseDeepLink(url: string): DeepLinkAction | null {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return null;
  }
  if (parsed.protocol !== `${DEEP_LINK_PROTOCOL}:`) return null;

  if (parsed.hostname === 'auth' && parsed.pathname === '/callback') {
    const token = parsed.searchParams.get('token');
    if (token) return { kind: 'auth-token', token };
    const linkToken = parsed.searchParams.get('link_token');
    if (linkToken) return { kind: 'auth-link-token', linkToken };
    const mfaToken = parsed.searchParams.get('mfa_token');
    if (mfaToken) return { kind: 'auth-mfa-token', mfaToken };
    const error = parsed.searchParams.get('error');
    if (error) return { kind: 'auth-error', code: error };
    return null;
  }

  if (parsed.hostname === 'invite') {
    const id = parsed.pathname.replace(/^\//, '');
    if (id) return { kind: 'invite', invitationId: decodeURIComponent(id) };
    return null;
  }

  return null;
}

// Чистая классификация результата пробы аутентификации демона. Без импортов
// Electron, чтобы тестироваться в jsdom: отличает «демон не может
// аутентифицироваться» от «демон медленный / сеть недоступна / упал».

export interface AuthProbeOutcome {
  status?: number;
  noToken?: boolean;
  networkError?: boolean;
}

export type AuthProbeResult = 'auth_expired' | 'ok' | 'unknown';

export function isAuthStatusError(err: unknown): boolean {
  return typeof err === 'object' && err !== null && (err as { status?: unknown }).status === 401;
}

export function classifyAuthProbe(outcome: AuthProbeOutcome): AuthProbeResult {
  if (outcome.noToken) return 'auth_expired';
  if (outcome.networkError) return 'unknown';
  if (outcome.status === 401) return 'auth_expired';
  if (outcome.status !== undefined && outcome.status >= 200 && outcome.status < 300) {
    return 'ok';
  }
  return 'unknown';
}

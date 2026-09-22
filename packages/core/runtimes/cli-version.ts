/**
 * Зеркало серверной проверки MinQuickCreateCLIVersion. Модалка быстрого
 * создания агента требует, чтобы CLI goosar в демоне был не старше этой
 * версии — иначе возможны дублирование issue при сбое, потеря вложений или
 * неверная обработка вставленных ссылок на скриншоты. Сервер — источник
 * истины, фронтенд лишь заранее предупреждает пользователя об устаревшем
 * демоне.
 */
export const MIN_QUICK_CREATE_CLI_VERSION = '0.2.21';
export const MIN_QUICK_CREATE_FIELDS_CLI_VERSION = '0.4.3';

export type CliVersionState = 'ok' | 'too_old' | 'missing';

export interface CliVersionCheck {
  state: CliVersionState;
  current: string;
  min: string;
}

const SEMVER_RE = /v?(\d+)\.(\d+)\.(\d+)/;

const DEV_DESCRIBE_RE = /^v?\d+\.\d+\.\d+-\d+-g[0-9a-fA-F]+/;

function parseSemver(raw: string): [number, number, number] | null {
  const m = SEMVER_RE.exec(raw.trim());
  if (!m) return null;
  return [Number(m[1]), Number(m[2]), Number(m[3])];
}

function lessThan(a: [number, number, number], b: [number, number, number]) {
  if (a[0] !== b[0]) return a[0] < b[0];
  if (a[1] !== b[1]) return a[1] < b[1];
  return a[2] < b[2];
}

export function checkQuickCreateCliVersion(detected: string | undefined | null): CliVersionCheck {
  return checkCliVersion(detected, MIN_QUICK_CREATE_CLI_VERSION);
}

export function checkQuickCreateFieldsCliVersion(
  detected: string | undefined | null,
): CliVersionCheck {
  return checkCliVersion(detected, MIN_QUICK_CREATE_FIELDS_CLI_VERSION);
}

function checkCliVersion(detected: string | undefined | null, minimum: string): CliVersionCheck {
  const current = (detected ?? '').trim();
  if (DEV_DESCRIBE_RE.test(current)) {
    return { state: 'ok', current, min: minimum };
  }
  const parsed = current ? parseSemver(current) : null;
  if (!parsed) {
    return { state: 'missing', current, min: minimum };
  }
  const min = parseSemver(minimum)!;
  if (lessThan(parsed, min)) {
    return { state: 'too_old', current, min: minimum };
  }
  return { state: 'ok', current, min: minimum };
}

export function readRuntimeCliVersion(metadata: Record<string, unknown> | undefined): string {
  const v = metadata?.cli_version;
  return typeof v === 'string' ? v : '';
}

export const MIN_HANDOFF_CLI_VERSION = '0.3.28';

export function handoffSupported(detected: string | undefined | null): boolean {
  return meetsMinCliVersion(detected, MIN_HANDOFF_CLI_VERSION);
}

export const MIN_CHAT_PROJECT_CONTEXT_CLI_VERSION = '0.4.10';

export function chatProjectContextSupported(detected: string | undefined | null): boolean {
  return meetsMinCliVersion(detected, MIN_CHAT_PROJECT_CONTEXT_CLI_VERSION);
}

function meetsMinCliVersion(detected: string | undefined | null, minimum: string): boolean {
  const current = (detected ?? '').trim();
  if (!current) return false;
  if (DEV_DESCRIBE_RE.test(current)) return true;
  const parsed = parseSemver(current);
  if (!parsed) return false;
  return !lessThan(parsed, parseSemver(minimum)!);
}

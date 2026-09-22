// start_date / due_date задачи — календарные дни, а не моменты времени.

const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})/;

function pad(n: number): string {
  return String(n).padStart(2, '0');
}

export function toDateOnly(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function todayDateOnly(): string {
  return toDateOnly(new Date());
}

export function addDaysDateOnly(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return toDateOnly(d);
}

function parseParts(value: string): [number, number, number] | null {
  const m = DATE_ONLY.exec(value);
  if (m) return [Number(m[1]), Number(m[2]), Number(m[3])];
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return null;
  return [d.getUTCFullYear(), d.getUTCMonth() + 1, d.getUTCDate()];
}

export function dateOnlyToUTCDate(value: string | null | undefined): Date | null {
  if (!value) return null;
  const parts = parseParts(value);
  if (!parts) return null;
  return new Date(Date.UTC(parts[0], parts[1] - 1, parts[2]));
}

export function dateOnlyToLocalDate(value: string | null | undefined): Date | undefined {
  if (!value) return undefined;
  const parts = parseParts(value);
  if (!parts) return undefined;
  return new Date(parts[0], parts[1] - 1, parts[2]);
}

export function formatDateOnly(
  value: string | null | undefined,
  options: Intl.DateTimeFormatOptions,
  locale: string,
): string {
  const d = dateOnlyToUTCDate(value);
  if (!d) return '';
  return d.toLocaleDateString(locale, { ...options, timeZone: 'UTC' });
}

export function isPastDateOnly(value: string | null | undefined): boolean {
  const d = dateOnlyToUTCDate(value);
  if (!d) return false;
  const today = dateOnlyToUTCDate(todayDateOnly());
  return today != null && d.getTime() < today.getTime();
}

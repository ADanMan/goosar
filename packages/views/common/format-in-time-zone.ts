// Показ момента времени по часам расписания, а не читателя: автопилот на 18:00
// America/Los_Angeles не должен показывать 09:00 читателю в UTC+8.
export function formatInTimeZone(
  iso: string,
  timeZone: string | undefined,
  locale: string,
): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  const options: Intl.DateTimeFormatOptions = {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  };
  try {
    return new Intl.DateTimeFormat(locale, { ...options, timeZone }).format(at);
  } catch {
    return new Intl.DateTimeFormat(locale, options).format(at);
  }
}

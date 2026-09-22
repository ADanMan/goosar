// Документ пользовательских настроек Kerberos, общий для main (владеет файлом)
// и renderer (редактирует через IPC). Секретов здесь нет.
export interface KerberosPreferences {
  principal: string | null;
  thresholdMs: number;
  snoozedUntil: number | null;
}

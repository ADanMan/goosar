import { useCallback, useEffect, useState } from 'react';
import { Check, CircleHelp, Loader2, Minus, RefreshCw, X } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { cn } from '@goosar/ui/lib/utils';
import { useT, useUiLocale } from '@goosar/views/i18n';
import type { DoctorReport, DoctorRow } from '../../../shared/daemon-types';

const RED = new Set(['missing', 'outdated']);

function rank(row: DoctorRow): number {
  if (RED.has(row.status)) return 0;
  if (row.status === 'ok') return 1;
  return 2;
}

function StatusIcon({ status }: { status: string }) {
  if (RED.has(status)) return <X className="size-3.5 text-destructive" aria-hidden />;
  if (status === 'ok') return <Check className="size-3.5 text-emerald-600" aria-hidden />;
  if (status === 'skipped') return <Minus className="size-3.5 text-muted-foreground" aria-hidden />;
  return <CircleHelp className="size-3.5 text-muted-foreground" aria-hidden />;
}

export function useDoctorReport() {
  const [report, setReport] = useState<DoctorReport | null | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const supported = typeof window.daemonAPI?.doctor === 'function';

  const load = useCallback(
    async (refresh: boolean) => {
      if (!supported) return;
      setBusy(true);
      try {
        setReport((await window.daemonAPI.doctor({ refresh })) ?? null);
      } catch {
        setReport(null);
      } finally {
        setBusy(false);
      }
    },
    [supported],
  );

  useEffect(() => {
    void load(false);
  }, [load]);

  return { report, busy, supported, refresh: () => load(true) };
}

export function DoctorSection() {
  const { t } = useT('settings');
  const locale = useUiLocale();
  const { report, busy, supported, refresh } = useDoctorReport();
  if (!supported) return null;

  const rows = report ? [...report.results].sort((a, b) => rank(a) - rank(b)) : [];

  return (
    <div className="border-t border-border pt-2.5">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-medium">{t(($) => $.desktop.perimeter.doctor_title)}</p>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.desktop.perimeter.doctor_description)}
          </p>
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="size-7 shrink-0"
          aria-label={t(($) => $.desktop.perimeter.doctor_refresh)}
          title={t(($) => $.desktop.perimeter.doctor_refresh)}
          disabled={busy}
          onClick={() => void refresh()}
        >
          {busy ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <RefreshCw className="size-3.5" />
          )}
        </Button>
      </div>

      {report === undefined && busy ? null : report === null ? (
        <p className="mt-2 text-xs text-muted-foreground">
          {t(($) => $.desktop.perimeter.doctor_unavailable)}
        </p>
      ) : (
        <>
          <ul className="mt-2 flex flex-col gap-1.5">
            {rows.map((row) => {
              const red = RED.has(row.status);
              return (
                <li key={row.id} className="min-w-0 text-xs">
                  <div className="flex items-start gap-1.5">
                    <span className="mt-0.5 shrink-0">
                      <StatusIcon status={row.status} />
                    </span>
                    <div className="min-w-0 flex-1">
                      <span className={cn('font-medium', red && 'text-destructive')}>
                        {row.name}
                      </span>
                      {row.detail && !red ? (
                        <span className="ml-1 break-all text-muted-foreground">{row.detail}</span>
                      ) : null}
                      {red ? (
                        <>
                          <p className="text-muted-foreground">{row.message}</p>
                          {row.fix ? (
                            <p className="mt-0.5">
                              <span className="text-muted-foreground">
                                {t(($) => $.desktop.perimeter.doctor_fix)}:{' '}
                              </span>
                              <span className="break-words">{row.fix}</span>
                            </p>
                          ) : null}
                        </>
                      ) : null}
                    </div>
                  </div>
                </li>
              );
            })}
          </ul>
          {report ? (
            <p className="mt-1.5 text-[11px] text-muted-foreground">
              {t(($) => $.desktop.perimeter.doctor_checked_at, {
                time: new Intl.DateTimeFormat(locale, { timeStyle: 'short' }).format(
                  new Date(report.checkedAt),
                ),
              })}
            </p>
          ) : null}
        </>
      )}
    </div>
  );
}

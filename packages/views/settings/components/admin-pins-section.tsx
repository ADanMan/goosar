'use client';

import { useState } from 'react';
import { toast } from 'sonner';
import { useQuery } from '@tanstack/react-query';
import { Info, TriangleAlert, X } from 'lucide-react';
import { Badge } from '@goosar/ui/components/ui/badge';
import { Button } from '@goosar/ui/components/ui/button';
import { Switch } from '@goosar/ui/components/ui/switch';
import { useCurrentWorkspace } from '@goosar/core/paths';
import {
  provisioningCatalogOptions,
  provisioningPinsOptions,
  useUpdateProvisioningPins,
} from '@goosar/core/workspace/admin-config';
import {
  UNKNOWN_CATALOG,
  knownCatalog,
  pinCatalogStatus,
  pinOutcome,
  blockerIsPinned,
  packageKey,
  provisioningBlocker,
} from '@goosar/core/provisioning';
import type {
  CatalogState,
  PackageIdentity,
  PinAction,
  PinCatalogStatus,
  PinOutcome,
} from '@goosar/core/provisioning';
import type { ProvisioningPin, ProvisioningPinInput } from '@goosar/core/api/workspace-admin';
import { useT } from '../../i18n';
import { SettingsCard, SettingsSection } from './settings-layout';
import { TypedConfirmDialog } from './typed-confirm-dialog';

interface PinRow {
  name: string;
  type: string;
  version: string;
  pin: ProvisioningPin | null;
  status: PinCatalogStatus;
}

function pinKey(type: string, name: string): string {
  return `${type}:${name}`;
}

function rowIdentity(row: PinRow): PackageIdentity {
  return { type: row.type, name: row.name, version: row.version };
}

function join(...parts: (string | null)[]): string {
  return parts.filter((p): p is string => p !== null && p !== '').join(' ');
}

export function AdminPinsSection({ wsId }: { wsId: string }) {
  const { t } = useT('settings');
  const workspace = useCurrentWorkspace();
  const { data: pinsView } = useQuery(provisioningPinsOptions(wsId));
  const { data: catalogView } = useQuery(provisioningCatalogOptions(wsId));
  const updatePins = useUpdateProvisioningPins(wsId);

  const pins = pinsView?.pins ?? [];
  const workspaceName = workspace?.name ?? '';

  const catalog: CatalogState = catalogView
    ? knownCatalog(catalogView.packages ?? [])
    : UNKNOWN_CATALOG;

  const [action, setAction] = useState<PinAction | null>(null);
  const outcome = action === null ? null : pinOutcome(pins, catalog, action);

  const rowsByKey = new Map<string, PinRow>();
  if (catalog.known) {
    for (const pkg of catalog.packages) {
      const key = pinKey(pkg.type, pkg.name);
      const existing = rowsByKey.get(key);
      if (!existing || pkg.version > existing.version) {
        rowsByKey.set(key, {
          name: pkg.name,
          type: pkg.type,
          version: pkg.version,
          pin: null,
          status: 'published',
        });
      }
    }
  }
  for (const pin of pins) {
    rowsByKey.set(pinKey(pin.package_type, pin.package_name), {
      name: pin.package_name,
      type: pin.package_type,
      version: pin.version,
      pin,
      status: pinCatalogStatus(pin, catalog),
    });
  }
  const rows = [...rowsByKey.values()].sort((a, b) =>
    a.type === b.type ? (a.name < b.name ? -1 : 1) : a.type < b.type ? -1 : 1,
  );

  const hasPins = pins.length > 0;
  const allDisabled = hasPins && pins.every((p) => p.enabled !== true);
  const blocker = provisioningBlocker(pins, catalog);

  const submit = async (next: ProvisioningPinInput[]): Promise<boolean> => {
    try {
      await updatePins.mutateAsync(next);
      toast.success(t(($) => $.admin.pins.toast_saved));
      return true;
    } catch {
      toast.error(t(($) => $.admin.pins.toast_save_failed));
      return false;
    }
  };

  const perform = (candidate: PinAction) => {
    const next = pinOutcome(pins, catalog, candidate);
    if (next.gate === 'blocked') return;
    if (next.gate === 'none') {
      void submit(next.nextPins);
      return;
    }
    setAction(candidate);
  };

  const confirm = async (open: PinOutcome) => {
    if (await submit(open.nextPins)) setAction(null);
  };

  const toggleAction = (row: PinRow): PinAction =>
    row.pin?.enabled === true
      ? { kind: 'disable', target: rowIdentity(row) }
      : { kind: 'enable', target: rowIdentity(row) };

  const lockfileLine = (open: PinOutcome): string => {
    const act = open.action;
    switch (act.kind) {
      case 'enable':
        return t(($) => $.admin.pins.lockfile_pin, { name: act.target.name });
      case 'disable':
        return t(($) => $.admin.pins.lockfile_disable, {
          name: act.target.name,
        });
      case 'unpin':
        return t(($) => $.admin.pins.lockfile_unpin, { name: act.target.name });
      case 'reset':
        return t(($) => $.admin.pins.lockfile_reset, { total: pins.length });
    }
  };

  const uncertain = (open: PinOutcome): boolean =>
    open.effect === 'unknown' || open.effect === 'breaks' || open.effect === 'still-broken';

  const dialogTitle = (open: PinOutcome): string => {
    if (open.action.kind === 'reset') {
      return t(($) => $.admin.pins.reset_dialog_title);
    }
    const name = open.action.target.name;
    if (open.effect === 'repair') {
      return t(($) => $.admin.pins.repair_dialog_title);
    }
    switch (open.action.kind) {
      case 'enable':
        return !uncertain(open) && open.effect === 'narrow'
          ? t(($) => $.admin.pins.first_pin_dialog_title, { name })
          : t(($) => $.admin.pins.pin_neutral_title, { name });
      case 'unpin':
        if (uncertain(open)) {
          return t(($) => $.admin.pins.unpin_neutral_title, { name });
        }
        if (open.effect === 'stop' && !open.becomesUnpinned) {
          return t(($) => $.admin.pins.disable_last_dialog_title);
        }
        if (open.becomesUnpinned) {
          return t(($) => $.admin.pins.unpin_last_dialog_title, { name });
        }
        return open.targetRevoked
          ? t(($) => $.admin.pins.unpin_dialog_title, { name })
          : t(($) => $.admin.pins.unpin_neutral_title, { name });
      case 'disable':
        if (uncertain(open)) {
          return t(($) => $.admin.pins.disable_neutral_title, { name });
        }
        if (open.effect === 'stop') {
          return t(($) => $.admin.pins.disable_last_dialog_title);
        }
        return open.targetRevoked
          ? t(($) => $.admin.pins.disable_dialog_title, { name })
          : t(($) => $.admin.pins.disable_neutral_title, { name });
    }
  };

  const resetDescription = (open: PinOutcome): string => {
    const total = pins.length;
    if (open.effect === 'unknown') {
      return join(
        lockfileLine(open),
        t(($) => $.admin.pins.effect_unknown),
      );
    }
    if (open.effect === 'breaks') {
      return join(
        lockfileLine(open),
        t(($) => $.admin.pins.effect_breaks),
      );
    }
    if (open.effect === 'still-broken') {
      return join(
        lockfileLine(open),
        t(($) => $.admin.pins.effect_still_broken),
      );
    }
    const widen = t(($) => $.admin.pins.reset_dialog_description, { total });
    return open.effect === 'repair'
      ? join(
          t(($) => $.admin.pins.effect_repair_unpinned),
          widen,
        )
      : widen;
  };

  const collateralLine = (open: PinOutcome): string | null => {
    if (open.action.kind === 'reset') return null;
    const targetKey = packageKey(open.action.target);
    const others = open.revoked.filter((p) => packageKey(p) !== targetKey);
    if (others.length === 0) return null;
    return t(($) => $.admin.pins.collateral_line, {
      name: open.action.target.name,
      names: others.map((p) => `${p.name}@${p.version}`).join(', '),
    });
  };

  const dialogDescription = (open: PinOutcome): string => {
    if (open.action.kind === 'reset') return resetDescription(open);
    const name = open.action.target.name;
    const lockfile = lockfileLine(open);
    if (open.effect === 'unknown') {
      return join(
        lockfile,
        t(($) => $.admin.pins.effect_unknown),
      );
    }
    if (open.effect === 'breaks') {
      return join(
        lockfile,
        t(($) => $.admin.pins.effect_breaks),
      );
    }
    if (open.effect === 'still-broken') {
      return join(
        lockfile,
        t(($) => $.admin.pins.effect_still_broken),
      );
    }
    if (open.effect === 'repair') {
      return join(
        lockfile,
        open.becomesUnpinned
          ? t(($) => $.admin.pins.effect_repair_unpinned)
          : t(($) => $.admin.pins.effect_repair),
        open.staleTargetDropped ? t(($) => $.admin.pins.effect_stale_dropped, { name }) : null,
      );
    }
    switch (open.action.kind) {
      case 'enable':
        return open.effect === 'narrow'
          ? t(($) => $.admin.pins.first_pin_dialog_description, { name })
          : t(($) => $.admin.pins.first_pin_no_change_description, { name });
      case 'unpin':
        if (open.becomesUnpinned) {
          return open.staleTargetDropped
            ? join(
                lockfile,
                t(($) => $.admin.pins.effect_stale_dropped, { name }),
              )
            : t(($) => $.admin.pins.unpin_last_dialog_description, { name });
        }
        if (open.effect === 'stop') {
          return t(($) => $.admin.pins.unpin_stop_dialog_description, { name });
        }
        if (open.targetKept) {
          return t(($) => $.admin.pins.unpin_kept_dialog_description, { name });
        }
        return open.targetRevoked
          ? join(
              t(($) => $.admin.pins.unpin_dialog_description, { name }),
              collateralLine(open),
            )
          : t(($) => $.admin.pins.unpin_inactive_dialog_description, { name });
      case 'disable':
        if (open.effect === 'stop') {
          return t(($) => $.admin.pins.disable_last_dialog_description, {
            name,
          });
        }
        if (open.targetKept) {
          return t(($) => $.admin.pins.disable_kept_dialog_description, {
            name,
          });
        }
        return open.targetRevoked
          ? join(
              t(($) => $.admin.pins.disable_dialog_description, { name }),
              collateralLine(open),
            )
          : lockfile;
    }
  };

  const dialogConfirmLabel = (open: PinOutcome): string => {
    if (open.action.kind === 'reset') {
      return t(($) => $.admin.pins.reset_confirm);
    }
    if (open.effect === 'repair') {
      return t(($) => $.admin.pins.repair_confirm);
    }
    const neutral = t(($) => $.admin.pins.neutral_confirm);
    if (uncertain(open)) return neutral;
    switch (open.action.kind) {
      case 'enable':
        return open.effect === 'narrow' ? t(($) => $.admin.pins.first_pin_confirm) : neutral;
      case 'unpin':
        if (open.effect === 'stop' && !open.becomesUnpinned) {
          return t(($) => $.admin.pins.disable_last_confirm);
        }
        if (open.becomesUnpinned) {
          return t(($) => $.admin.pins.unpin_last_confirm);
        }
        return open.targetRevoked ? t(($) => $.admin.pins.unpin_confirm) : neutral;
      case 'disable':
        if (open.effect === 'stop') {
          return t(($) => $.admin.pins.disable_last_confirm);
        }
        return open.targetRevoked ? t(($) => $.admin.pins.disable_confirm) : neutral;
    }
  };

  const dialogProps = (open: PinOutcome) => {
    const wide = open.gate === 'workspace';
    return {
      loading: updatePins.isPending,
      onClose: () => setAction(null),
      inputId:
        open.action.kind === 'reset'
          ? 'reset-pins-confirm'
          : open.action.kind === 'disable'
            ? 'disable-pin-confirm'
            : open.action.kind === 'unpin'
              ? 'unpin-confirm'
              : 'first-pin-confirm',
      title: dialogTitle(open),
      description: dialogDescription(open),
      target: open.action.kind === 'reset' || wide ? workspaceName : open.action.target.name,
      unavailableNote: wide
        ? t(($) => $.admin.pins.workspace_unavailable)
        : t(($) => $.admin.pins.unpin_unavailable),
      confirmLabel: dialogConfirmLabel(open),
      cancelLabel:
        open.action.kind === 'reset'
          ? t(($) => $.admin.pins.reset_cancel)
          : open.action.kind === 'disable'
            ? t(($) => $.admin.pins.disable_cancel)
            : open.action.kind === 'unpin'
              ? t(($) => $.admin.pins.unpin_cancel)
              : t(($) => $.admin.pins.first_pin_cancel),
      onConfirm: () => void confirm(open),
    };
  };

  const switchLabel = (row: PinRow, enable: PinOutcome | null): string => {
    if (row.pin?.enabled === true) {
      return t(($) => $.admin.pins.toggle_off_aria, { name: row.name });
    }
    if (enable?.effect === 'narrow') {
      return t(($) => $.admin.pins.toggle_on_narrow_aria, { name: row.name });
    }
    return t(($) => $.admin.pins.toggle_on_aria, { name: row.name });
  };

  const blockedNote = (row: PinRow): string => {
    if (row.status === 'version-missing') {
      return t(($) => $.admin.pins.stale_version_enable_blocked, {
        name: row.name,
        version: row.version,
      });
    }
    if (row.status === 'missing') {
      return t(($) => $.admin.pins.stale_enable_blocked);
    }
    return t(($) => $.admin.pins.requires_enable_blocked, { name: row.name });
  };

  return (
    <SettingsSection
      title={t(($) => $.admin.pins.title)}
      description={t(($) => $.admin.pins.description)}
      action={
        hasPins ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => perform({ kind: 'reset' })}
            disabled={updatePins.isPending}
          >
            {t(($) => $.admin.pins.reset_pins)}
          </Button>
        ) : undefined
      }
    >
      {!catalog.known && (
        <div className="flex items-start gap-2 rounded-md border border-surface-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{t(($) => $.admin.pins.catalog_unknown_banner)}</span>
        </div>
      )}
      {blocker !== null && (
        <div className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs">
          <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>
            {t(
              ($) =>
                blockerIsPinned(pins, blocker)
                  ? $.admin.pins.broken_banner
                  : 
                    $.admin.pins.broken_banner_dependency,
              { name: blocker.name, version: blocker.version },
            )}
          </span>
        </div>
      )}
      {!hasPins && (
        <div className="flex items-start gap-2 rounded-md border border-surface-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{t(($) => $.admin.pins.no_pins_banner)}</span>
        </div>
      )}
      {allDisabled && (
        <div className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs">
          <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{t(($) => $.admin.pins.all_disabled_banner)}</span>
        </div>
      )}
      <SettingsCard>
        {rows.length === 0 && (
          <div className="px-4 py-3 text-sm text-muted-foreground">
            {t(($) => $.admin.pins.empty_catalog)}
          </div>
        )}
        {rows.map((row) => {
          const enable =
            row.pin?.enabled === true
              ? null
              : pinOutcome(pins, catalog, {
                  kind: 'enable',
                  target: rowIdentity(row),
                });
          const enableBlocked = enable?.gate === 'blocked';
          return (
            <div key={pinKey(row.type, row.name)} className="flex items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium">{row.name}</span>
                  <Badge variant="outline">{row.type}</Badge>
                  {row.status === 'missing' && (
                    <Badge variant="destructive">{t(($) => $.admin.pins.stale_badge)}</Badge>
                  )}
                  {row.status === 'version-missing' && (
                    <Badge variant="destructive">
                      {t(($) => $.admin.pins.stale_version_badge)}
                    </Badge>
                  )}
                </div>
                <div className="text-xs text-muted-foreground">{row.version}</div>
                {enableBlocked && (
                  <p className="mt-1 text-xs text-muted-foreground">{blockedNote(row)}</p>
                )}
              </div>
              {row.pin && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t(($) => $.admin.pins.unpin_aria, {
                    name: row.name,
                  })}
                  onClick={() => perform({ kind: 'unpin', target: rowIdentity(row) })}
                  disabled={updatePins.isPending}
                >
                  <X className="h-4 w-4 text-muted-foreground" />
                </Button>
              )}
              <Switch
                checked={row.pin?.enabled === true}
                onCheckedChange={() => perform(toggleAction(row))}
                aria-label={switchLabel(row, enable)}
                disabled={updatePins.isPending || enableBlocked}
              />
            </div>
          );
        })}
      </SettingsCard>
      {outcome !== null && <TypedConfirmDialog {...dialogProps(outcome)} />}
    </SettingsSection>
  );
}

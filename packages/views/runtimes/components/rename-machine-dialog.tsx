'use client';

import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { useUpdateRuntime } from '@goosar/core/runtimes/mutations';
import { AlertDialog, AlertDialogContent } from '@goosar/ui/components/ui/alert-dialog';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { useT } from '../../i18n';

export interface RenameMachineDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  wsId: string;
  runtimeId: string;
  currentName: string;
}

export function RenameMachineDialog({
  open,
  onOpenChange,
  wsId,
  runtimeId,
  currentName,
}: RenameMachineDialogProps) {
  const { t } = useT('runtimes');
  const updateRuntime = useUpdateRuntime(wsId);

  const [value, setValue] = useState(currentName);
  const submitting = updateRuntime.isPending;

  useEffect(() => {
    if (open) setValue(currentName);
  }, [open, currentName]);

  const handleOpenChange = (next: boolean) => {
    if (submitting) return;
    onOpenChange(next);
  };

  const handleSave = () => {
    const trimmed = value.trim();
    updateRuntime.mutate(
      {
        runtimeId,
        patch: { custom_name: trimmed, apply_to_machine: true },
      },
      {
        onSuccess: () => {
          toast.success(
            trimmed
              ? t(($) => $.machine.rename_dialog.toast_saved)
              : t(($) => $.machine.rename_dialog.toast_cleared),
          );
          onOpenChange(false);
        },
        onError: (err) =>
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.machine.rename_dialog.toast_failed),
          ),
      },
    );
  };

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent
        className="w-[calc(100vw-2rem)] !max-w-[440px] gap-0 overflow-hidden rounded-lg p-0"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="px-5 pb-4 pt-5">
          <h2 className="text-base font-semibold">{t(($) => $.machine.rename_dialog.title)}</h2>
          <p className="mt-1 text-sm leading-5 text-muted-foreground">
            {t(($) => $.machine.rename_dialog.description)}
          </p>

          <Input
            className="mt-3"
            autoFocus
            value={value}
            maxLength={100}
            placeholder={t(($) => $.machine.rename_dialog.placeholder)}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !submitting) {
                e.preventDefault();
                handleSave();
              }
            }}
          />
          <p className="mt-1.5 text-xs text-muted-foreground">
            {t(($) => $.machine.rename_dialog.hint)}
          </p>
        </div>

        <div className="border-t bg-muted/25 px-5 py-3">
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto"
              onClick={() => handleOpenChange(false)}
              disabled={submitting}
            >
              {t(($) => $.machine.rename_dialog.cancel)}
            </Button>
            <Button
              type="button"
              className="w-full sm:w-auto"
              onClick={handleSave}
              disabled={submitting}
            >
              {submitting
                ? t(($) => $.machine.rename_dialog.saving)
                : t(($) => $.machine.rename_dialog.save)}
            </Button>
          </div>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  );
}

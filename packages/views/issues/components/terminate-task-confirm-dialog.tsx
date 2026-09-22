'use client';

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@goosar/ui/components/ui/alert-dialog';
import { useT } from '../../i18n';

interface TerminateTaskConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  showRunningNote?: boolean;
}

export function TerminateTaskConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  showRunningNote = false,
}: TerminateTaskConfirmDialogProps) {
  const { t } = useT('issues');

  if (!open) return null;

  return (
    <AlertDialog open onOpenChange={onOpenChange}>
      <AlertDialogContent
        onClick={(e) => e.stopPropagation()}
      >
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.terminate_dialog.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.terminate_dialog.body)}
            {showRunningNote && t(($) => $.terminate_dialog.running_note)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.terminate_dialog.keep)}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={() => {
              onOpenChange(false);
              onConfirm();
            }}
          >
            {t(($) => $.terminate_dialog.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

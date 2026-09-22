'use client';

import { useEffect, useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@goosar/ui/components/ui/dialog';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { Button } from '@goosar/ui/components/ui/button';
import { isImeComposing } from '@goosar/core/utils';
import { useT } from '../../i18n';

export function DeleteWorkspaceDialog({
  workspaceName,
  loading = false,
  open,
  onOpenChange,
  onConfirm,
}: {
  workspaceName: string;
  loading?: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  const { t } = useT('settings');
  const [typed, setTyped] = useState('');
  const matched = workspaceName !== '' && typed === workspaceName;

  useEffect(() => {
    setTyped('');
  }, [open, workspaceName]);

  const submit = () => {
    if (!matched || loading) return;
    onConfirm();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.delete_workspace_dialog.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.delete_workspace_dialog.description)}</DialogDescription>
        </DialogHeader>

        {workspaceName === '' ? (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.admin.typed_confirm.target_unavailable)}
          </p>
        ) : (
          <div className="space-y-2">
            <Label htmlFor="delete-workspace-confirm" className="text-xs">
              {t(($) => $.delete_workspace_dialog.type_to_confirm_prefix)}{' '}
              <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">
                {workspaceName}
              </code>{' '}
              {t(($) => $.delete_workspace_dialog.type_to_confirm_suffix)}
            </Label>
            <Input
              id="delete-workspace-confirm"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              onKeyDown={(e) => {
                if (isImeComposing(e)) return;
                if (e.key === 'Enter') {
                  e.preventDefault();
                  submit();
                }
              }}
              placeholder={t(($) => $.admin.typed_confirm.type_placeholder)}
              autoFocus
              disabled={loading}
              autoComplete="off"
              autoCorrect="off"
              autoCapitalize="off"
              spellCheck={false}
            />
          </div>
        )}

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={loading}
          >
            {t(($) => $.delete_workspace_dialog.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={submit}
            disabled={!matched || loading}
          >
            {loading
              ? t(($) => $.delete_workspace_dialog.deleting)
              : t(($) => $.delete_workspace_dialog.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

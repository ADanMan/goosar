'use client';

import { useEffect, useRef, useState } from 'react';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@goosar/ui/components/ui/dialog';
import { isImeComposing } from '@goosar/core/utils';
import { useT } from '../../i18n';

export function TypedConfirmDialog({
  inputId,
  title,
  description,
  target,
  unavailableNote,
  confirmLabel,
  cancelLabel,
  loading,
  onClose,
  onConfirm,
}: {
  inputId: string;
  title: string;
  description: string;
  target: string;
  unavailableNote: string;
  confirmLabel: string;
  cancelLabel: string;
  loading: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT('settings');
  const [typed, setTyped] = useState('');
  const inputRef = useRef<HTMLInputElement | null>(null);
  const matched = target !== '' && typed === target;

  useEffect(() => {
    const id = setTimeout(() => inputRef.current?.focus(), 0);
    return () => clearTimeout(id);
  }, []);

  const submit = () => {
    if (!matched || loading) return;
    onConfirm();
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !loading) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          {/* The target is unbounded server-side and lands inside a max-w-sm
              popup, so the heading wraps like the label does. */}
          <DialogTitle className="break-words">{title}</DialogTitle>
          <DialogDescription className="break-words">{description}</DialogDescription>
        </DialogHeader>

        {target === '' ? (
          <p className="text-xs text-muted-foreground">{unavailableNote}</p>
        ) : (
          <div className="space-y-2">
            <Label htmlFor={inputId} className="flex-wrap text-xs">
              {t(($) => $.admin.typed_confirm.type_prefix)}{' '}
              <code className="min-w-0 break-all rounded bg-muted px-1 py-0.5 font-mono text-xs">
                {target}
              </code>{' '}
              {t(($) => $.admin.typed_confirm.type_suffix)}
            </Label>
            <Input
              id={inputId}
              ref={inputRef}
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
          <Button type="button" variant="outline" onClick={onClose} disabled={loading}>
            {cancelLabel}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={submit}
            disabled={!matched || loading}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

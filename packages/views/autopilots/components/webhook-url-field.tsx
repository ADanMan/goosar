'use client';

import { useState, type ReactNode } from 'react';
import { Check, Copy, Eye, EyeOff } from 'lucide-react';
import { toast } from 'sonner';
import { maskAutopilotWebhookUrl } from '@goosar/core/autopilots';
import { Button } from '@goosar/ui/components/ui/button';
import { cn } from '@goosar/ui/lib/utils';
import { copyText } from '@goosar/ui/lib/clipboard';
import { useT } from '../../i18n';

const SIZES = {
  sm: {
    row: 'items-center',
    value: 'rounded bg-muted px-2 py-1 text-xs',
    button: 'h-7 w-7',
    buttonVariant: 'ghost',
    icon: 'h-3.5 w-3.5',
  },
  md: {
    row: 'items-stretch',
    value: 'rounded-md border bg-muted px-3 py-2 text-xs',
    button: 'h-9 w-9',
    buttonVariant: 'outline',
    icon: 'size-4',
  },
} as const;

interface WebhookUrlFieldProps {
  url: string;
  size?: keyof typeof SIZES;
  actions?: ReactNode;
}

export function WebhookUrlField({ url, size = 'sm', actions }: WebhookUrlFieldProps) {
  const { t } = useT('autopilots');
  const [revealedUrl, setRevealedUrl] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const revealed = revealedUrl === url;
  const s = SIZES[size];

  const handleCopy = async () => {
    if (!url) return;
    if (await copyText(url)) {
      setCopied(true);
      toast.success(t(($) => $.trigger_row.url_copied));
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t(($) => $.trigger_row.url_copy_failed));
    }
  };

  const valueClassName = cn('flex-1 min-w-0 truncate font-mono text-foreground', s.value);

  return (
    <div className={cn('flex gap-1.5', s.row)}>
      {revealed ? (
        <code className={valueClassName}>{url}</code>
      ) : (
        <button
          type="button"
          onClick={() => setRevealedUrl(url)}
          className={cn(
            valueClassName,
            'text-left cursor-pointer transition-colors hover:bg-muted/70',
          )}
          title={t(($) => $.trigger_row.show_url)}
          aria-label={t(($) => $.trigger_row.hidden_url_aria)}
        >
          {maskAutopilotWebhookUrl(url)}
        </button>
      )}
      <Button
        size="icon"
        variant={s.buttonVariant}
        className={cn('shrink-0', s.button)}
        onClick={() => setRevealedUrl(revealed ? null : url)}
        title={revealed ? t(($) => $.trigger_row.hide_url) : t(($) => $.trigger_row.show_url)}
        aria-label={revealed ? t(($) => $.trigger_row.hide_url) : t(($) => $.trigger_row.show_url)}
      >
        {revealed ? (
          <EyeOff className={cn(s.icon, 'text-muted-foreground')} />
        ) : (
          <Eye className={cn(s.icon, 'text-muted-foreground')} />
        )}
      </Button>
      <Button
        size="icon"
        variant={s.buttonVariant}
        className={cn('shrink-0', s.button)}
        onClick={handleCopy}
        title={t(($) => $.trigger_row.copy_url)}
        aria-label={t(($) => $.trigger_row.copy_url)}
      >
        {copied ? (
          <Check className={cn(s.icon, 'text-emerald-500')} />
        ) : (
          <Copy className={cn(s.icon, 'text-muted-foreground')} />
        )}
      </Button>
      {actions}
    </div>
  );
}

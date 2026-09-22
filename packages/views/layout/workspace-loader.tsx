'use client';

import { GoosarIcon } from '@goosar/ui/components/common/goosar-icon';
import { useT } from '../i18n';

export function WorkspaceLoader({ name }: { name?: string | null }) {
  const { t } = useT('layout');
  return (
    <div
      className="flex h-svh w-full items-center justify-center bg-background"
      aria-live="polite"
      role="status"
    >
      <div className="flex flex-col items-center gap-4">
        <GoosarIcon className="size-10 animate-pulse" />
        {name ? (
          <p className="text-sm text-muted-foreground">
            {t(($) => $.workspace_loader.loading_named_prefix)}{' '}
            <span className="font-medium text-foreground">{name}</span>…
          </p>
        ) : (
          <p className="text-sm text-muted-foreground">
            {t(($) => $.workspace_loader.loading_workspace)}
          </p>
        )}
      </div>
    </div>
  );
}

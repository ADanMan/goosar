'use client';

import { Download, ExternalLink, Maximize2, Trash2 } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { paths, useWorkspaceSlug } from '@goosar/core/paths';
import { useT } from '../i18n';
import { useNavigation } from '../navigation';
import { useAttachmentHtmlText } from './hooks/use-attachment-html-text';
import { HtmlPreviewBody } from './html-preview-body';

const PREVIEW_HEIGHT = 'h-[480px]';
const ERROR_PLACEHOLDER_HEIGHT = 'h-20';

interface HtmlAttachmentPreviewProps {
  attachmentId: string;
  filename: string;
  onPreview: () => void;
  onDownload: () => void;
  onDelete?: () => void;
}

export function HtmlAttachmentPreview({
  attachmentId,
  filename,
  onPreview,
  onDownload,
  onDelete,
}: HtmlAttachmentPreviewProps) {
  const { t } = useT('editor');
  const query = useAttachmentHtmlText(attachmentId);
  const isError = !query.isLoading && (!!query.error || !query.data?.text);
  const slug = useWorkspaceSlug();
  const navigation = useNavigation();

  const canOpenInNewTab = !!slug && !!attachmentId;
  const handleOpenInNewTab = () => {
    if (!slug) return;
    const nameQuery = filename ? `?name=${encodeURIComponent(filename)}` : '';
    const path = `${paths.workspace(slug).attachmentPreview(attachmentId)}${nameQuery}`;
    if (navigation.openInNewTab) {
      navigation.openInNewTab(path, filename, { activate: true });
      return;
    }
    const url = navigation.getShareableUrl(path);
    window.open(url, '_blank', 'noopener,noreferrer');
  };

  return (
    <div className="group/html-preview relative my-1" onMouseDown={(e) => e.stopPropagation()}>
      <HtmlPreviewBody
        source={{ kind: 'attachment', attachmentId }}
        title={filename}
        className={PREVIEW_HEIGHT}
        placeholderClassName={isError ? ERROR_PLACEHOLDER_HEIGHT : PREVIEW_HEIGHT}
        errorTestId="html-attachment-preview-error"
      />
      <div
        className={cn(
          'absolute right-2 top-2 flex items-center gap-0.5 rounded-md border border-border bg-background/95 p-0.5 shadow-sm transition-opacity',
          isError ? 'opacity-100' : 'opacity-0 group-hover/html-preview:opacity-100',
        )}
      >
        <button
          type="button"
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          title={t(($) => $.attachment.preview)}
          aria-label={t(($) => $.attachment.preview)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onPreview();
          }}
        >
          <Maximize2 className="h-3.5 w-3.5" />
        </button>
        {canOpenInNewTab && (
          <button
            type="button"
            className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            title={t(($) => $.attachment.open_in_new_tab)}
            aria-label={t(($) => $.attachment.open_in_new_tab)}
            onMouseDown={(e) => {
              e.preventDefault();
              e.stopPropagation();
              handleOpenInNewTab();
            }}
          >
            <ExternalLink className="h-3.5 w-3.5" />
          </button>
        )}
        <button
          type="button"
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          title={t(($) => $.image.download)}
          aria-label={t(($) => $.image.download)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onDownload();
          }}
        >
          <Download className="h-3.5 w-3.5" />
        </button>
        {onDelete && (
          <button
            type="button"
            className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
            title={t(($) => $.attachment.remove)}
            aria-label={t(($) => $.attachment.remove)}
            onMouseDown={(e) => {
              e.preventDefault();
              e.stopPropagation();
              onDelete();
            }}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}

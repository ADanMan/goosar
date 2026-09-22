'use client';

import { Download, Eye, FileText, Loader2, Trash2 } from 'lucide-react';
import { useT } from '../i18n';
import { getPreviewKind } from './utils/preview';

interface AttachmentCardChromeProps {
  filename: string;
  uploading?: boolean;
  canPreview: boolean;
  canDownload: boolean;
  canDelete?: boolean;
  onPreview: () => void;
  onDownload: () => void;
  onDelete?: () => void;
}

function AttachmentCardChrome({
  filename,
  uploading,
  canPreview,
  canDownload,
  canDelete,
  onPreview,
  onDownload,
  onDelete,
}: AttachmentCardChromeProps) {
  const { t } = useT('editor');
  return (
    <div
      className="flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted"
      onMouseDown={(e) => e.stopPropagation()}
    >
      {uploading ? (
        <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" />
      ) : (
        <FileText className="size-4 shrink-0 text-muted-foreground" />
      )}
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm">
          {uploading ? t(($) => $.file_card.uploading, { filename }) : filename}
        </p>
      </div>
      {!uploading && canPreview && (
        <button
          type="button"
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          title={t(($) => $.attachment.preview)}
          aria-label={t(($) => $.attachment.preview)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onPreview();
          }}
        >
          <Eye className="size-3.5" />
        </button>
      )}
      {!uploading && canDownload && (
        <button
          type="button"
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          title={t(($) => $.image.download)}
          aria-label={t(($) => $.image.download)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onDownload();
          }}
        >
          <Download className="size-3.5" />
        </button>
      )}
      {!uploading && canDelete && onDelete && (
        <button
          type="button"
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
          title={t(($) => $.attachment.remove)}
          aria-label={t(($) => $.attachment.remove)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onDelete();
          }}
        >
          <Trash2 className="size-3.5" />
        </button>
      )}
    </div>
  );
}

export interface AttachmentCardProps {
  filename: string;
  contentType?: string;
  attachmentId?: string;
  href?: string;
  uploading?: boolean;
  onPreview: () => void;
  onDownload: () => void;
  onDelete?: () => void;
}

export function AttachmentCard({
  filename,
  contentType = '',
  attachmentId,
  href,
  uploading,
  onPreview,
  onDownload,
  onDelete,
}: AttachmentCardProps) {
  const kind = filename ? getPreviewKind(contentType, filename) : null;
  const isUrlPreviewableKind = kind === 'pdf' || kind === 'video' || kind === 'audio';
  const canPreview = !!href && kind !== null && (!!attachmentId || isUrlPreviewableKind);

  return (
    <div className="my-1">
      <AttachmentCardChrome
        filename={filename}
        uploading={uploading}
        canPreview={canPreview}
        canDownload={!!href}
        canDelete={!!onDelete}
        onPreview={onPreview}
        onDownload={onDownload}
        onDelete={onDelete}
      />
    </div>
  );
}

'use client';

import { Download, Link as LinkIcon, Maximize2, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { cn } from '@goosar/ui/lib/utils';
import { copyText } from '@goosar/ui/lib/clipboard';
import { useQuery } from '@tanstack/react-query';
import { api } from '@goosar/core/api';
import { useConfigStore } from '@goosar/core/config';
import type { Attachment as AttachmentRecord } from '@goosar/core/types';
import { attachmentIdFromDownloadURL } from '@goosar/core/types/attachment-url';
import { useT } from '../i18n';
import { useAttachmentDownloadResolver } from './attachment-download-context';
import { useAttachmentPreview } from './attachment-preview-modal';
import { useDownloadAttachment } from './use-download-attachment';
import { AttachmentCard } from './attachment-card';
import { HtmlAttachmentPreview } from './html-attachment-preview';
import { getPreviewKind, type PreviewKind } from './utils/preview';
import './styles/attachment.css';

export type AttachmentInput =
  | { kind: 'record'; attachment: AttachmentRecord }
  // Markdown / Tiptap inline: only a URL + filename. Resolves to a full
  // record via the surrounding AttachmentDownloadProvider when available;
  // otherwise renders in URL-only mode (media types still preview from URL,
  // text types fall back to a download CTA).
  | {
      kind: 'url';
      url: string;
      filename: string;
      contentType?: string;
      uploading?: boolean;
      width?: number;
      height?: number;
      forceKind?: PreviewKind;
    };

export interface AttachmentProps {
  attachment: AttachmentInput;
  editable?: boolean;
  selected?: boolean;
  onDelete?: () => void;
  className?: string;
}

interface Normalized {
  filename: string;
  contentType: string;
  url: string;
  attachmentId?: string;
  record?: AttachmentRecord;
  uploading: boolean;
  width?: number;
  height?: number;
}

function normalize(
  input: AttachmentInput,
  resolve: (url: string) => AttachmentRecord | undefined,
  cdnDomain: string,
  cdnSigned: boolean,
): Normalized {
  if (input.kind === 'record') {
    return {
      filename: input.attachment.filename,
      contentType: input.attachment.content_type,
      url: absolutizeMediaURL(
        pickInlineMediaURL(input.attachment, input.attachment.url, cdnDomain, cdnSigned),
      ),
      attachmentId: input.attachment.id,
      record: input.attachment,
      uploading: false,
    };
  }
  const record = input.url ? resolve(input.url) : undefined;
  return {
    filename: input.filename || record?.filename || '',
    contentType: input.contentType || record?.content_type || '',
    url: absolutizeMediaURL(
      record ? pickInlineMediaURL(record, input.url, cdnDomain, cdnSigned) : input.url,
    ),
    attachmentId: record?.id,
    record,
    uploading: !!input.uploading,
    width: input.width,
    height: input.height,
  };
}

function absolutizeMediaURL(rawUrl: string): string {
  if (!rawUrl) return rawUrl;
  if (/^https?:\/\//i.test(rawUrl)) return rawUrl;
  if (/^blob:/i.test(rawUrl) || /^data:/i.test(rawUrl)) return rawUrl;
  if (!rawUrl.startsWith('/')) return rawUrl;
  const baseUrl = (api.getBaseUrl?.() ?? '').replace(/\/+$/, '');
  if (!baseUrl) return rawUrl;
  return `${baseUrl}${rawUrl}`;
}

function pickInlineMediaURL(
  record: AttachmentRecord,
  fallback: string,
  cdnDomain: string,
  cdnSigned: boolean,
): string {
  const dl = record.download_url ?? '';
  if (
    /^https?:\/\//i.test(dl) &&
    /[?&](Signature|X-Amz-Signature|Key-Pair-Id|Expires|X-Amz-Expires)=/i.test(dl)
  ) {
    return dl;
  }
  if (!cdnSigned && storageURLMatchesCdnDomain(record.url, cdnDomain)) return record.url;
  if (isSiteRelativeLocalUploadURL(record.url)) return record.url;
  if (record.markdown_url) return record.markdown_url;
  if (record.url) return record.url;
  return fallback;
}

function isSiteRelativeLocalUploadURL(rawURL: string): boolean {
  if (!rawURL || !rawURL.startsWith('/')) return false;
  const path = rawURL.split(/[?#]/, 1)[0] ?? '';
  return path === '/uploads' || path.startsWith('/uploads/');
}

function storageURLMatchesCdnDomain(rawURL: string, cdnDomain: string): boolean {
  const expected = normalizeHost(cdnDomain);
  if (!rawURL || !expected) return false;
  try {
    const u = new URL(rawURL);
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return false;
    if (normalizeHost(u.hostname) !== expected) return false;
    return !hasExpiringSignatureQuery(u.searchParams);
  } catch {
    return false;
  }
}

function normalizeHost(host: string): string {
  return host.trim().toLowerCase().replace(/\.$/, '');
}

function hasExpiringSignatureQuery(q: URLSearchParams): boolean {
  for (const key of ['Signature', 'X-Amz-Signature', 'Key-Pair-Id', 'Expires', 'X-Amz-Expires']) {
    if (q.has(key)) return true;
  }
  return false;
}

const RESIGN_STALE_MS = 20 * 60 * 1000;

function useResignedInlineMediaURL(attachmentId: string | undefined, pickedUrl: string): string {
  const idFromPickedUrl = attachmentIdFromDownloadURL(pickedUrl);
  const resignAttachmentId = attachmentId ?? idFromPickedUrl;
  const needsResign =
    !!resignAttachmentId &&
    !!pickedUrl &&
    idFromPickedUrl !== undefined &&
    (api.getBaseUrl?.() ?? '') !== '';

  const { data: fresh } = useQuery({
    queryKey: ['attachment-inline-resign', resignAttachmentId],
    queryFn: () => api.getAttachment(resignAttachmentId as string),
    enabled: needsResign,
    staleTime: RESIGN_STALE_MS,
    gcTime: RESIGN_STALE_MS,
  });

  if (!needsResign) return pickedUrl;
  const dl = fresh?.download_url ?? '';
  if (/^https?:\/\//i.test(dl) && attachmentIdFromDownloadURL(dl) === undefined) {
    return dl;
  }
  return pickedUrl;
}

export function Attachment({
  attachment,
  editable,
  selected,
  onDelete,
  className,
}: AttachmentProps) {
  const { resolveAttachment, openByUrl } = useAttachmentDownloadResolver();
  const cdnDomain = useConfigStore((s) => s.cdnDomain);
  const cdnSigned = useConfigStore((s) => s.cdnSigned);
  const download = useDownloadAttachment();
  const preview = useAttachmentPreview();

  const state = normalize(attachment, resolveAttachment, cdnDomain, cdnSigned);
  const mediaUrl = useResignedInlineMediaURL(state.attachmentId, state.url);
  const forceKind = attachment.kind === 'url' ? attachment.forceKind : undefined;
  const kind =
    forceKind ??
    (state.filename || state.contentType
      ? getPreviewKind(state.contentType, state.filename)
      : null);

  const openPreview = () => {
    if (state.record) {
      preview.tryOpen({
        kind: 'full',
        attachment: {
          ...state.record,
          download_url: mediaUrl || state.record.download_url,
        },
      });
      return;
    }
    if (mediaUrl) {
      preview.tryOpen({
        kind: 'url',
        url: mediaUrl,
        filename: state.filename,
      });
    }
  };

  const handleDownload = () => {
    if (state.attachmentId) {
      download(state.attachmentId);
      return;
    }
    if (mediaUrl) openByUrl(mediaUrl);
  };

  if (kind === 'image') {
    return (
      <>
        <ImageAttachmentView
          src={mediaUrl}
          alt={state.filename}
          uploading={state.uploading}
          width={state.width}
          height={state.height}
          editable={editable}
          selected={selected}
          onView={openPreview}
          onDownload={handleDownload}
          onDelete={onDelete}
          className={className}
        />
        {preview.modal}
      </>
    );
  }

  if (kind === 'html' && state.attachmentId && !state.uploading) {
    return (
      <>
        <HtmlAttachmentPreview
          attachmentId={state.attachmentId}
          filename={state.filename}
          onPreview={openPreview}
          onDownload={handleDownload}
          onDelete={editable ? onDelete : undefined}
        />
        {preview.modal}
      </>
    );
  }

  return (
    <>
      <AttachmentCard
        filename={state.filename}
        contentType={state.contentType}
        attachmentId={state.attachmentId}
        href={mediaUrl || undefined}
        uploading={state.uploading}
        onPreview={openPreview}
        onDownload={handleDownload}
        onDelete={editable ? onDelete : undefined}
      />
      {preview.modal}
    </>
  );
}

interface ImageAttachmentViewProps {
  src: string;
  alt: string;
  uploading: boolean;
  width?: number;
  height?: number;
  editable?: boolean;
  selected?: boolean;
  onView: () => void;
  onDownload: () => void;
  onDelete?: () => void;
  className?: string;
}

function ImageAttachmentView({
  src,
  alt,
  uploading,
  width,
  height,
  editable,
  selected,
  onView,
  onDownload,
  onDelete,
  className,
}: ImageAttachmentViewProps) {
  const { t } = useT('editor');

  const handleCopyLink = async () => {
    if (await copyText(src)) {
      toast.success(t(($) => $.image.link_copied));
    } else {
      toast.error(t(($) => $.image.copy_link_failed));
    }
  };

  const clickable = !editable && !uploading;

  return (
    <span className="image-node">
      <span
        className={cn('image-figure', selected && editable && 'image-selected', className)}
        data-clickable={clickable || undefined}
        contentEditable={false}
        onClick={clickable ? onView : undefined}
      >
        <img
          src={src || undefined}
          alt={alt}
          width={width}
          height={height}
          className={cn('image-content', uploading && 'image-uploading')}
          draggable={false}
        />
        {!uploading && src && (
          <span
            className="image-toolbar"
            onMouseDown={(e) => e.stopPropagation()}
            onClick={(e) => e.stopPropagation()}
          >
            <button type="button" onClick={onView} title={t(($) => $.image.view)}>
              <Maximize2 className="size-3.5" />
            </button>
            <button type="button" onClick={onDownload} title={t(($) => $.image.download)}>
              <Download className="size-3.5" />
            </button>
            <button type="button" onClick={handleCopyLink} title={t(($) => $.image.copy_link)}>
              <LinkIcon className="size-3.5" />
            </button>
            {editable && onDelete && (
              <button type="button" onClick={onDelete} title={t(($) => $.image.delete)}>
                <Trash2 className="size-3.5" />
              </button>
            )}
          </span>
        )}
      </span>
    </span>
  );
}

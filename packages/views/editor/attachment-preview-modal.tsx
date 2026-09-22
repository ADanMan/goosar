'use client';

import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { AnimatePresence, motion, useReducedMotion } from 'motion/react';
import { PreviewTooLargeError, PreviewUnsupportedError } from '@goosar/core/api';
import { Download, ExternalLink, FileText, Loader2, X } from 'lucide-react';
import type { Attachment } from '@goosar/core/types';
import { paths, useWorkspaceSlug } from '@goosar/core/paths';
import { cn } from '@goosar/ui/lib/utils';
import { resolvePublicFileUrl } from '@goosar/core/workspace/avatar-url';
import { UI_EASE_OUT, UI_MOTION_DURATION } from '@goosar/ui/lib/motion';
import { useT } from '../i18n';
import { useNavigation } from '../navigation';
import { openExternal } from '../platform';
import { ReadonlyContent } from './readonly-content';
import { extensionToLanguage, getPreviewKind, type PreviewKind } from './utils/preview';
import { useDownloadAttachment } from './use-download-attachment';
import { useAttachmentHtmlText } from './hooks/use-attachment-html-text';
import { useZoomCanvas, type ZoomCanvasApi } from './hooks/use-zoom-canvas';
import { ZoomCanvas, ZoomControls } from './zoom-canvas';
import type { Size } from './utils/zoom-transform';
import { HtmlPreviewBody } from './html-preview-body';
import { CodeBlockStatic } from './code-block-static';

export type PreviewSource =
  { kind: 'full'; attachment: Attachment } | { kind: 'url'; url: string; filename: string };

const URL_ONLY_KINDS = new Set<PreviewKind>(['image', 'pdf', 'video', 'audio']);

interface PreviewState {
  filename: string;
  contentType: string;
  mediaUrl: string;
  attachmentId: string | null;
}

function resolvePreviewMediaUrl(attachment: Attachment): string {
  const raw = attachment.download_url || attachment.markdown_url || attachment.url;
  return resolvePublicFileUrl(raw) ?? raw;
}

function normalize(source: PreviewSource): PreviewState {
  if (source.kind === 'full') {
    return {
      filename: source.attachment.filename,
      contentType: source.attachment.content_type,
      mediaUrl: resolvePreviewMediaUrl(source.attachment),
      attachmentId: source.attachment.id,
    };
  }
  return {
    filename: source.filename,
    contentType: '',
    mediaUrl: resolvePublicFileUrl(source.url) ?? source.url,
    attachmentId: null,
  };
}

interface AttachmentPreviewModalProps {
  source: PreviewSource;
  open: boolean;
  onClose: () => void;
}

export interface AttachmentPreviewHandle {
  tryOpen: (source: PreviewSource) => boolean;
  open: (source: PreviewSource) => void;
  modal: ReactNode;
}

export function useAttachmentPreview(): AttachmentPreviewHandle {
  const [current, setCurrent] = useState<PreviewSource | null>(null);
  const [previewOpen, setPreviewOpen] = useState(false);

  const open = useCallback((source: PreviewSource) => {
    setCurrent(source);
    setPreviewOpen(true);
  }, []);
  const tryOpen = useCallback((source: PreviewSource) => {
    const state = normalize(source);
    const kind = getPreviewKind(state.contentType, state.filename);
    if (!kind) return false;
    if (source.kind === 'url' && !URL_ONLY_KINDS.has(kind)) return false;
    setCurrent(source);
    setPreviewOpen(true);
    return true;
  }, []);

  const modal = useMemo(
    () =>
      current ? (
        <AttachmentPreviewModal
          source={current}
          open={previewOpen}
          onClose={() => setPreviewOpen(false)}
          onExitComplete={() => setCurrent(null)}
        />
      ) : null,
    [current, previewOpen],
  );

  return useMemo(() => ({ open, tryOpen, modal }), [open, tryOpen, modal]);
}

export function AttachmentPreviewModal({
  source,
  open,
  onClose,
  onExitComplete,
}: AttachmentPreviewModalProps & { onExitComplete?: () => void }) {
  const download = useDownloadAttachment();
  const shouldReduceMotion = useReducedMotion() ?? false;
  const state = normalize(source);
  const slug = useWorkspaceSlug();
  const navigation = useNavigation();

  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [open, onClose]);

  const kind = getPreviewKind(state.contentType, state.filename);

  const handleDownload = () => {
    if (state.attachmentId) {
      download(state.attachmentId);
    } else {
      openExternal(state.mediaUrl);
    }
  };

  const canOpenInNewTab = kind === 'html' && !!slug && !!state.attachmentId;
  const handleOpenInNewTab = () => {
    if (!slug || !state.attachmentId) return;
    const nameQuery = state.filename ? `?name=${encodeURIComponent(state.filename)}` : '';
    const path = `${paths.workspace(slug).attachmentPreview(state.attachmentId)}${nameQuery}`;
    if (navigation.openInNewTab) {
      navigation.openInNewTab(path, state.filename, { activate: true });
    } else {
      const url = navigation.getShareableUrl(path);
      window.open(url, '_blank', 'noopener,noreferrer');
    }
    onClose();
  };

  if (typeof document === 'undefined') return null;

  return createPortal(
    <AnimatePresence onExitComplete={onExitComplete}>
      {open && (
        <motion.div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
          onClick={(e) => {
            if (e.target === e.currentTarget) onClose();
          }}
          role="dialog"
          aria-modal="true"
          aria-label={state.filename}
          initial={{ opacity: 0 }}
          animate={{
            opacity: 1,
            transition: {
              duration: UI_MOTION_DURATION.fast,
              ease: UI_EASE_OUT,
            },
          }}
          exit={{
            opacity: 0,
            transition: {
              duration: UI_MOTION_DURATION.fast,
              ease: UI_EASE_OUT,
            },
          }}
        >
          {/* Larger than the create-issue dialog (max-w-4xl, manualDialogContentClass)
              because PDF / video previews want more room. Capped to viewport
              minus the surrounding p-4 (1rem each side) so it never overflows
              the screen on small displays / split panes. */}
          <motion.div
            className="flex h-[min(90vh,calc(100vh-2rem))] w-full max-w-6xl flex-col overflow-hidden rounded-lg bg-background shadow-xl"
            onClick={(e) => e.stopPropagation()}
            initial={{
              opacity: 0,
              transform: shouldReduceMotion ? 'scale(1)' : 'scale(0.95)',
            }}
            animate={{
              opacity: 1,
              transform: 'scale(1)',
              transition: {
                duration: UI_MOTION_DURATION.standard,
                ease: UI_EASE_OUT,
              },
            }}
            exit={{
              opacity: 0,
              transform: shouldReduceMotion ? 'scale(1)' : 'scale(0.95)',
              transition: {
                duration: UI_MOTION_DURATION.fast,
                ease: UI_EASE_OUT,
              },
            }}
          >
            {/* Below the `open &&` gate on purpose: the panel's zoom state is
                destroyed on close, so every open re-fits instead of restoring
                a stale zoom from the last time this image was viewed. */}
            <PreviewPanel
              kind={kind}
              source={source}
              state={state}
              onClose={onClose}
              onDownload={handleDownload}
              onOpenInNewTab={canOpenInNewTab ? handleOpenInNewTab : undefined}
            />
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>,
    document.body,
  );
}

function PreviewPanel({
  kind,
  source,
  state,
  onClose,
  onDownload,
  onOpenInNewTab,
}: {
  kind: PreviewKind | null;
  source: PreviewSource;
  state: PreviewState;
  onClose: () => void;
  onDownload: () => void;
  onOpenInNewTab?: () => void;
}) {
  const { t } = useT('editor');

  const [measured, setMeasured] = useState<{ url: string; size: Size } | null>(null);
  const natural = kind === 'image' && measured?.url === state.mediaUrl ? measured.size : null;
  const canvas = useZoomCanvas({ content: natural });

  const handleNaturalSize = useCallback((url: string, size: Size) => {
    setMeasured((previous) =>
      previous?.url === url &&
      previous.size.width === size.width &&
      previous.size.height === size.height
        ? previous
        : { url, size },
    );
  }, []);

  return (
    <>
      <div className="flex items-center gap-2 border-b border-border bg-muted/30 px-4 py-2">
        <FileText className="size-4 shrink-0 text-muted-foreground" />
        <p className="truncate text-sm font-medium">{state.filename}</p>
        <span className="ml-1 shrink-0 text-xs text-muted-foreground">
          {state.contentType || '—'}
        </span>
        <div className="ml-auto flex items-center gap-1">
          {/* Only once the image has been measured: without a natural size
              there is no canvas behind these buttons to drive. */}
          {natural && <ZoomControls canvas={canvas} className="mr-1" />}
          {onOpenInNewTab && (
            <button
              type="button"
              className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title={t(($) => $.attachment.open_in_new_tab)}
              aria-label={t(($) => $.attachment.open_in_new_tab)}
              onClick={onOpenInNewTab}
            >
              <ExternalLink className="size-4" />
            </button>
          )}
          <button
            type="button"
            className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            title={t(($) => $.image.download)}
            aria-label={t(($) => $.image.download)}
            onClick={onDownload}
          >
            <Download className="size-4" />
          </button>
          <button
            type="button"
            className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            title={t(($) => $.attachment.close)}
            aria-label={t(($) => $.attachment.close)}
            onClick={onClose}
          >
            <X className="size-4" />
          </button>
        </div>
      </div>
      {/* Image gets a flex column: the canvas sizes itself with `flex: 1 1
          auto` and its content is absolutely positioned, so in a plain block
          parent it would collapse to zero height and show nothing. It also
          clips and handles its own wheel events — letting this wrapper scroll
          too would fight the pan. Every other kind keeps the block scroller;
          making them flex items would let tall text previews shrink to fit
          instead of scrolling. */}
      <div
        className={cn(
          'min-h-0 flex-1 bg-background',
          kind === 'image' ? 'flex flex-col overflow-hidden' : 'overflow-auto',
        )}
      >
        {kind === 'image' ? (
          <ImagePreview
            state={state}
            canvas={canvas}
            natural={natural}
            onNaturalSize={handleNaturalSize}
          />
        ) : (
          <PreviewContent kind={kind} source={source} state={state} onDownload={onDownload} />
        )}
      </div>
    </>
  );
}

function ImagePreview({
  state,
  canvas,
  natural,
  onNaturalSize,
}: {
  state: PreviewState;
  canvas: ZoomCanvasApi;
  natural: Size | null;
  onNaturalSize: (url: string, size: Size) => void;
}) {
  const { t } = useT('editor');
  const url = state.mediaUrl;

  const readNaturalSize = useCallback(
    (image: HTMLImageElement | null) => {
      if (!image || image.naturalWidth <= 0 || image.naturalHeight <= 0) return;
      onNaturalSize(url, {
        width: image.naturalWidth,
        height: image.naturalHeight,
      });
    },
    [onNaturalSize, url],
  );

  return (
    <ZoomCanvas
      canvas={canvas}
      content={natural}
      label={t(($) => $.image.canvas_label)}
      className="bg-black/40"
      autoFocus
    >
      <img
        ref={readNaturalSize}
        onLoad={(e) => readNaturalSize(e.currentTarget)}
        src={url}
        alt={state.filename}
        className={cn(
          'select-none',
          natural ? 'block size-full' : 'max-h-full max-w-full rounded-lg object-contain',
        )}
        draggable={false}
      />
    </ZoomCanvas>
  );
}

function PreviewContent({
  kind,
  source,
  state,
  onDownload,
}: {
  kind: Exclude<PreviewKind, 'image'> | null;
  source: PreviewSource;
  state: PreviewState;
  onDownload: () => void;
}) {
  const { t } = useT('editor');

  if (kind === null) {
    return (
      <UnsupportedFallback
        message={t(($) => $.attachment.preview_unsupported)}
        onDownload={onDownload}
      />
    );
  }

  if ((kind === 'markdown' || kind === 'html' || kind === 'text') && !state.attachmentId) {
    return (
      <UnsupportedFallback
        message={t(($) => $.attachment.preview_unsupported)}
        onDownload={onDownload}
      />
    );
  }

  switch (kind) {
    case 'pdf':
      return (
        <iframe
          src={state.mediaUrl}
          className="h-full w-full bg-background"
          title={state.filename}
        />
      );
    case 'video':
      return (
        <div className="flex h-full w-full items-center justify-center bg-black">
          <video src={state.mediaUrl} controls className="h-full w-full object-contain" />
        </div>
      );
    case 'audio':
      return (
        <div className="flex h-full w-full items-center justify-center p-8">
          <audio src={state.mediaUrl} controls className="w-full max-w-xl" />
        </div>
      );
    case 'markdown':
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) => (
            <ReadonlyContent
              content={text}
              className="px-6 py-4"
              attachments={source.kind === 'full' ? [source.attachment] : []}
            />
          )}
        />
      );
    case 'html':
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) => (
            <HtmlPreviewBody
              source={{ kind: 'inline', html: text }}
              title={state.filename}
              className="h-full w-full"
              iframeClassName="rounded-none border-0"
            />
          )}
        />
      );
    case 'text':
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) => (
            <CodeBlockStatic
              language={extensionToLanguage(state.filename)}
              body={text}
              className="px-6 py-4"
            />
          )}
        />
      );
  }
}

function TextBackedPreview({
  attachmentId,
  onDownload,
  render,
}: {
  attachmentId: string;
  onDownload: () => void;
  render: (text: string) => ReactNode;
}) {
  const { t } = useT('editor');
  const query = useAttachmentHtmlText(attachmentId);

  if (query.isLoading) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        {t(($) => $.attachment.preview_loading)}
      </div>
    );
  }
  if (query.error) {
    if (query.error instanceof PreviewTooLargeError) {
      return (
        <UnsupportedFallback
          message={t(($) => $.attachment.preview_too_large)}
          onDownload={onDownload}
        />
      );
    }
    if (query.error instanceof PreviewUnsupportedError) {
      return (
        <UnsupportedFallback
          message={t(($) => $.attachment.preview_unsupported)}
          onDownload={onDownload}
        />
      );
    }
    return (
      <UnsupportedFallback
        message={t(($) => $.attachment.preview_failed)}
        onDownload={onDownload}
      />
    );
  }
  if (!query.data) return null;
  return <>{render(query.data.text)}</>;
}

function UnsupportedFallback({ message, onDownload }: { message: string; onDownload: () => void }) {
  const { t } = useT('editor');
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-8 text-center">
      <FileText className="size-8 text-muted-foreground" />
      <p className="text-sm text-muted-foreground">{message}</p>
      <button
        type="button"
        className="inline-flex items-center gap-2 rounded-md border border-border bg-background px-3 py-1.5 text-sm transition-colors hover:bg-muted"
        onClick={onDownload}
      >
        <Download className="size-4" />
        {t(($) => $.image.download)}
      </button>
    </div>
  );
}

export { isPreviewable } from './utils/preview';

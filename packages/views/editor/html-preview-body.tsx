'use client';

import { cn } from '@goosar/ui/lib/utils';
import { PreviewTooLargeError, PreviewUnsupportedError } from '@goosar/core/api';
import { useT } from '../i18n';
import { CodeBlockIframe } from './code-block-iframe';
import { withFragmentNavShim } from './utils/iframe-fragment-nav';
import { useAttachmentHtmlText } from './hooks/use-attachment-html-text';

export type HtmlSource =
  { kind: 'inline'; html: string } | { kind: 'attachment'; attachmentId: string };

interface HtmlPreviewBodyProps {
  source: HtmlSource;
  title: string;
  className?: string;
  iframeClassName?: string;
  placeholderClassName?: string;
  errorTestId?: string;
}

export function HtmlPreviewBody({
  source,
  title,
  className,
  iframeClassName,
  placeholderClassName,
  errorTestId,
}: HtmlPreviewBodyProps) {
  if (source.kind === 'inline') {
    return (
      <CodeBlockIframe
        html={withFragmentNavShim(source.html)}
        title={title}
        heightClassName={className}
        className={iframeClassName}
      />
    );
  }
  return (
    <AttachmentBody
      attachmentId={source.attachmentId}
      title={title}
      className={className}
      iframeClassName={iframeClassName}
      placeholderClassName={placeholderClassName ?? className}
      errorTestId={errorTestId}
    />
  );
}

function AttachmentBody({
  attachmentId,
  title,
  className,
  iframeClassName,
  placeholderClassName,
  errorTestId,
}: {
  attachmentId: string;
  title: string;
  className?: string;
  iframeClassName?: string;
  placeholderClassName?: string;
  errorTestId?: string;
}) {
  const { t } = useT('editor');
  const query = useAttachmentHtmlText(attachmentId);

  if (query.isLoading) {
    return (
      <div
        className={cn(
          'flex items-center justify-center rounded-md border border-border bg-muted/30 text-xs text-muted-foreground',
          placeholderClassName,
        )}
      >
        {t(($) => $.attachment.preview_loading)}
      </div>
    );
  }

  if (query.error || !query.data) {
    const message =
      query.error instanceof PreviewTooLargeError
        ? t(($) => $.attachment.preview_too_large)
        : query.error instanceof PreviewUnsupportedError
          ? t(($) => $.attachment.preview_unsupported)
          : t(($) => $.attachment.preview_failed);
    return (
      <div
        className={cn(
          'flex items-center rounded-md border border-border bg-muted/30 px-3 text-xs text-muted-foreground',
          placeholderClassName,
        )}
        data-testid={errorTestId}
      >
        <span className="truncate">{message}</span>
      </div>
    );
  }

  return (
    <CodeBlockIframe
      html={withFragmentNavShim(query.data.text)}
      title={title}
      heightClassName={className}
      className={iframeClassName}
    />
  );
}

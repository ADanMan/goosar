'use client';

import { useEffect } from 'react';
import { useT } from '../i18n';
import { useAttachmentHtmlText } from '../editor/hooks/use-attachment-html-text';
import { withFragmentNavShim } from '../editor/utils/iframe-fragment-nav';

interface AttachmentPreviewPageProps {
  attachmentId: string;
  filename?: string;
}

export function AttachmentPreviewPage({ attachmentId, filename }: AttachmentPreviewPageProps) {
  const { t } = useT('editor');
  const query = useAttachmentHtmlText(attachmentId);

  useEffect(() => {
    if (filename) document.title = filename;
  }, [filename]);

  const text = query.data?.text;
  const isLoading = query.isLoading;
  const isError = !isLoading && (!!query.error || !text);

  return (
    <div className="flex h-full w-full flex-col bg-background">
      {isLoading ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          {t(($) => $.attachment.preview_loading)}
        </div>
      ) : isError ? (
        <div
          className="flex flex-1 items-center justify-center px-4 text-sm text-muted-foreground"
          data-testid="attachment-preview-page-error"
        >
          {t(($) => $.attachment.preview_failed)}
        </div>
      ) : (
        <iframe
          srcDoc={withFragmentNavShim(text)}
          sandbox="allow-scripts"
          title={filename ?? 'HTML attachment'}
          className="flex-1 w-full border-0 bg-background"
        />
      )}
    </div>
  );
}

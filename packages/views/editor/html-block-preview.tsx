'use client';

import { useState } from 'react';
import { Check, Code as CodeIcon, Copy, Eye, Maximize2 } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { copyText } from '@goosar/ui/lib/clipboard';
import { Dialog, DialogContent } from '@goosar/ui/components/ui/dialog';
import { useT } from '../i18n';
import { CodeBlockStatic } from './code-block-static';
import { HtmlPreviewBody } from './html-preview-body';

const CODE_BLOCK_IFRAME_HEIGHT = 'h-[480px]';

export const HTML_BLOCK_PREVIEW_HEIGHT_PX = 480;

const HTML_LANGUAGE_LABEL = 'html';

interface HtmlBlockPreviewProps {
  html: string;
  className?: string;
}

export function HtmlBlockPreview({ html, className }: HtmlBlockPreviewProps) {
  const { t } = useT('editor');
  const [view, setView] = useState<'preview' | 'source'>('preview');
  const [copied, setCopied] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);

  const handleCopy = async () => {
    if (!html) return;
    if (await copyText(html)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const toggleView = () => setView((v) => (v === 'preview' ? 'source' : 'preview'));

  return (
    <div className={cn('code-block-wrapper group/code relative my-3', className)}>
      <div className="absolute top-0 right-0 z-10 flex items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100 focus-within:opacity-100">
        <span className="text-xs text-muted-foreground select-none">{HTML_LANGUAGE_LABEL}</span>
        <button
          type="button"
          onClick={toggleView}
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          title={
            view === 'preview'
              ? t(($) => $.code_block.show_source)
              : t(($) => $.code_block.show_preview)
          }
          aria-label={
            view === 'preview'
              ? t(($) => $.code_block.show_source)
              : t(($) => $.code_block.show_preview)
          }
        >
          {view === 'preview' ? (
            <CodeIcon className="h-3.5 w-3.5" />
          ) : (
            <Eye className="h-3.5 w-3.5" />
          )}
        </button>
        {view === 'preview' && (
          <button
            type="button"
            onClick={() => setFullscreen(true)}
            className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
            title={t(($) => $.code_block.fullscreen)}
            aria-label={t(($) => $.code_block.fullscreen)}
          >
            <Maximize2 className="h-3.5 w-3.5" />
          </button>
        )}
        <button
          type="button"
          onClick={handleCopy}
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          title={t(($) => $.code_block.copy_code)}
          aria-label={t(($) => $.code_block.copy_code)}
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      </div>
      {view === 'preview' ? (
        <HtmlPreviewBody
          source={{ kind: 'inline', html }}
          title="HTML preview"
          className={CODE_BLOCK_IFRAME_HEIGHT}
        />
      ) : (
        <CodeBlockStatic language="xml" body={html} />
      )}
      <Dialog open={fullscreen} onOpenChange={setFullscreen}>
        <DialogContent
          className="!max-w-6xl !h-[min(90vh,calc(100vh-2rem))] w-full p-0 gap-0 overflow-hidden"
          aria-label={t(($) => $.code_block.fullscreen)}
        >
          <HtmlPreviewBody
            source={{ kind: 'inline', html }}
            title="HTML preview"
            className="h-full w-full"
            iframeClassName="rounded-none border-0"
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}

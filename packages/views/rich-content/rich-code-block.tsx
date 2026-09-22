'use client';

import { memo, useEffect, useMemo, useState, type ReactNode } from 'react';
import { toHtml } from 'hast-util-to-html';
import { Check, Copy } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { copyText } from '@goosar/ui/lib/clipboard';
import { useT } from '../i18n';
import {
  MermaidDiagram,
  MERMAID_SKELETON_HEIGHT_PX,
  reservedMermaidHeightPx,
} from '../editor/mermaid-diagram';
import { HtmlBlockPreview, HTML_BLOCK_PREVIEW_HEIGHT_PX } from '../editor/html-block-preview';
import { highlightCode } from '../editor/syntax-highlight';
import { LazyRichBlock } from './lazy-rich-block';

export type RichFenceLanguage = 'mermaid' | 'html';

export function isRichFenceLanguage(language: string | undefined): language is RichFenceLanguage {
  return language === 'mermaid' || language === 'html';
}

export function shouldUpgradeFence(language: string | undefined, isFenceClosed: boolean): boolean {
  return isRichFenceLanguage(language) && isFenceClosed;
}

const MemoMermaidDiagram = memo(MermaidDiagram);
const MemoHtmlBlockPreview = memo(HtmlBlockPreview);

export function StaticCodeBody({
  language,
  body,
  className,
}: {
  language: string | undefined;
  body: string;
  className?: string;
}) {
  const html = useMemo(() => {
    const code = body.replace(/\n$/, '');
    try {
      const tree = highlightCode(code, language);
      return toHtml(tree);
    } catch {
      return null;
    }
  }, [body, language]);

  if (html == null) {
    return <code className={cn('hljs', className)}>{body.replace(/\n$/, '')}</code>;
  }

  return (
    <code
      className={cn('hljs', language && `language-${language}`, className)}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}

export function CodeBlockShell({
  language,
  code,
  children,
}: {
  language?: string;
  code: string;
  children: ReactNode;
}) {
  const { t } = useT('editor');
  const [copied, setCopied] = useState(false);
  const copyLabel = t(($) => $.code_block.copy_code) || 'Copy code';

  const handleCopy = async () => {
    if (!code) return;
    if (await copyText(code)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className="code-block-wrapper group/code relative my-3">
      <div className="absolute top-0 right-0 z-10 flex items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100 focus-within:opacity-100">
        {language && <span className="text-xs text-muted-foreground select-none">{language}</span>}
        <button
          type="button"
          onClick={handleCopy}
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          title={copyLabel}
          aria-label={copyLabel}
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      </div>
      {/* No extra right padding: `.rich-text-editor pre` outranks utility
          padding classes anyway, and the editable NodeView uses the same
          1rem — keeping them identical keeps line wrapping identical. */}
      <pre className="!m-0">{children}</pre>
    </div>
  );
}

export function RichFenceBlock({ language, body }: { language: RichFenceLanguage; body: string }) {
  if (language === 'mermaid') return <MermaidFenceBlock chart={body} />;
  return <HtmlFenceBlock html={body} />;
}

function useReservedMermaidHeightPx(chart: string): number {
  const [height, setHeight] = useState(MERMAID_SKELETON_HEIGHT_PX);
  useEffect(() => {
    const cached = reservedMermaidHeightPx(chart);
    setHeight((current) => (current === cached ? current : cached));
  }, [chart]);
  return height;
}

function MermaidFenceBlock({ chart }: { chart: string }) {
  return (
    <LazyRichBlock reservedHeightPx={useReservedMermaidHeightPx(chart)} sourceKey={chart}>
      <MemoMermaidDiagram chart={chart} />
    </LazyRichBlock>
  );
}

function HtmlFenceBlock({ html }: { html: string }) {
  return (
    <LazyRichBlock reservedHeightPx={HTML_BLOCK_PREVIEW_HEIGHT_PX} sourceKey={html}>
      <MemoHtmlBlockPreview html={html} />
    </LazyRichBlock>
  );
}

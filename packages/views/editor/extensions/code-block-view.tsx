'use client';

import { useState } from 'react';
import { NodeViewWrapper, NodeViewContent } from '@tiptap/react';
import type { NodeViewProps } from '@tiptap/react';
import { Code as CodeIcon, Copy, Check, Eye } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { copyText } from '@goosar/ui/lib/clipboard';
import { useDebouncedValue } from '../../common/use-debounced-value';
import { useT } from '../../i18n';
import { MermaidDiagram } from '../mermaid-diagram';
import { CodeBlockIframe } from '../code-block-iframe';

const PREVIEW_DEBOUNCE_MS = 200;

const HTML_PREVIEW_HEIGHT = 'h-[480px]';

function CodeBlockView({ node }: NodeViewProps) {
  const { t } = useT('editor');
  const [copied, setCopied] = useState(false);
  const [view, setView] = useState<'preview' | 'source'>('preview');
  const language = node.attrs.language || '';
  const isMermaid = language === 'mermaid';
  const isHtml = language === 'html';
  const chart = node.textContent;
  const debouncedChart = useDebouncedValue(isMermaid ? chart : '', PREVIEW_DEBOUNCE_MS);
  const debouncedHtml = useDebouncedValue(isHtml ? chart : '', PREVIEW_DEBOUNCE_MS);

  const handleCopy = async () => {
    const text = node.textContent;
    if (!text) return;
    if (await copyText(text)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const showHtmlPreview = isHtml && view === 'preview';
  const toggleView = () => setView((v) => (v === 'preview' ? 'source' : 'preview'));

  return (
    <NodeViewWrapper className="code-block-wrapper group/code relative my-3">
      {isMermaid && debouncedChart.trim() && (
        <div contentEditable={false} className="mermaid-diagram-preview mb-1">
          <MermaidDiagram chart={debouncedChart} />
        </div>
      )}
      {isHtml && showHtmlPreview && (
        <div contentEditable={false} className="mb-1">
          <CodeBlockIframe
            html={debouncedHtml}
            title="HTML preview"
            heightClassName={HTML_PREVIEW_HEIGHT}
          />
        </div>
      )}
      <div
        contentEditable={false}
        className="code-block-header absolute top-0 right-0 z-10 flex items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100 focus-within:opacity-100"
      >
        {language && <span className="text-xs text-muted-foreground select-none">{language}</span>}
        {isHtml && (
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
      {/* `<pre>` + NodeViewContent must remain mounted so the user can keep
          editing the code block contents. When the HTML preview is showing
          we just visually hide it — ProseMirror still tracks it. */}
      <pre
        spellCheck={false}
        className={cn(showHtmlPreview && 'sr-only')}
        aria-hidden={showHtmlPreview ? 'true' : undefined}
      >
        {/* @ts-expect-error -- NodeViewContent supports as="code" at runtime */}
        <NodeViewContent as="code" />
      </pre>
    </NodeViewWrapper>
  );
}

export { CodeBlockView };

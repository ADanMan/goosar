import * as React from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import rehypeKatex from 'rehype-katex';
import rehypeRaw from 'rehype-raw';
import rehypeSanitize from 'rehype-sanitize';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import { FileText, Download } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { CODE_LIGATURE_CLASS } from '@goosar/ui/lib/code-style';
import { CodeBlock, InlineCode } from './CodeBlock';
import { isAllowedFileCardHref, preprocessFileCards } from './file-cards';
import { preprocessLinks } from './linkify';
import { preprocessIssueIdentifiers } from './issue-identifiers';
import { preprocessMentionShortcodes } from './mentions';
import { markdownSanitizeSchema, markdownUrlTransform } from './sanitize';
import { getMarkdownImagePolicy, useMarkdownImagePolicyVersion } from './image-policy';
import { BlockedImagePlaceholder } from './BlockedImage';
import 'katex/dist/katex.min.css';
import './markdown.css';

export type RenderMode = 'terminal' | 'minimal' | 'full';

export interface MarkdownProps {
  children: string;
  mode?: RenderMode;
  className?: string;
  id?: string;
  onUrlClick?: (url: string) => void;
  onFileClick?: (path: string) => void;
  renderMention?: (props: { type: string; id: string }) => React.ReactNode;
  cdnDomain?: string;
  renderImage?: (props: { src: string; alt: string }) => React.ReactNode;
  renderFileCard?: (props: { href: string; filename: string }) => React.ReactNode;
  autolinkIssueIdentifiers?: boolean;
}

const FILE_PATH_REGEX =
  /^(?:\/|~\/|\.\/)[\w\-./@]+\.(?:ts|tsx|js|jsx|mjs|cjs|md|json|yaml|yml|py|go|rs|css|scss|less|html|htm|txt|log|sh|bash|zsh|swift|kt|java|c|cpp|h|hpp|rb|php|xml|toml|ini|cfg|conf|env|sql|graphql|vue|svelte|astro|prisma)$/i;

function createComponents(
  mode: RenderMode,
  onUrlClick?: (url: string) => void,
  onFileClick?: (path: string) => void,
  renderMention?: (props: { type: string; id: string }) => React.ReactNode,
  renderImage?: (props: { src: string; alt: string }) => React.ReactNode,
  renderFileCard?: (props: { href: string; filename: string }) => React.ReactNode,
): Partial<Components> {
  const baseComponents: Partial<Components> = {
    div: ({ node, children, ...props }) => {
      const dataType = node?.properties?.dataType as string | undefined;
      if (dataType === 'fileCard') {
        const rawHref = (node?.properties?.dataHref as string) || '';
        const href = isAllowedFileCardHref(rawHref) ? rawHref : '';
        const filename = (node?.properties?.dataFilename as string) || '';
        if (renderFileCard) {
          return <>{renderFileCard({ href, filename })}</>;
        }
        return (
          <div className="my-1 flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted">
            <FileText className="size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm">{filename}</p>
            </div>
            {href && (
              <button
                type="button"
                className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                onClick={() => window.open(href, '_blank', 'noopener,noreferrer')}
              >
                <Download className="size-3.5" />
              </button>
            )}
          </div>
        );
      }
      return <div {...props}>{children}</div>;
    },
    img: ({ src, alt }) => {
      const srcString = typeof src === 'string' ? src : '';
      if (srcString === '' && getMarkdownImagePolicy().mode !== 'allow') {
        return <BlockedImagePlaceholder alt={alt ?? ''} />;
      }
      if (renderImage) {
        return <>{renderImage({ src: srcString, alt: alt ?? '' })}</>;
      }
      return (
        <img
          src={src}
          alt={alt ?? ''}
          className="max-w-full h-auto rounded-md my-2"
          loading="lazy"
        />
      );
    },
    a: ({ href, children }) => {
      if (href?.startsWith('mention://')) {
        const mentionMatch = href.match(/^mention:\/\/(member|agent|issue|project|all)\/(.+)$/);
        if (mentionMatch?.[1] && mentionMatch[2]) {
          const type = mentionMatch[1];
          const id = mentionMatch[2];

          if (renderMention) {
            const rendered = renderMention({ type, id });
            if (rendered) return <>{rendered}</>;
          }

          return <span className="text-primary font-semibold mx-0.5">{children}</span>;
        }
        return <span className="text-primary font-semibold mx-0.5">{children}</span>;
      }

      if (href?.startsWith('slash://skill/')) {
        return <span className="slash-command text-primary font-semibold mx-0.5">{children}</span>;
      }

      const handleClick = (e: React.MouseEvent): void => {
        e.preventDefault();
        if (href) {
          if (FILE_PATH_REGEX.test(href) && onFileClick) {
            onFileClick(href);
          } else if (onUrlClick) {
            onUrlClick(href);
          } else {
            window.open(href, '_blank', 'noopener,noreferrer');
          }
        }
      };

      return (
        <a
          href={href}
          onClick={handleClick}
          className="text-primary hover:underline cursor-pointer"
        >
          {children}
        </a>
      );
    },
  };

  if (mode === 'terminal') {
    return {
      ...baseComponents,
      code: ({ children }) => (
        <code className={cn('font-mono', CODE_LIGATURE_CLASS)}>{children}</code>
      ),
      pre: ({ children }) => (
        <pre className={cn('font-mono whitespace-pre-wrap my-2', CODE_LIGATURE_CLASS)}>
          {children}
        </pre>
      ),
      p: ({ children }) => <p className="my-1">{children}</p>,
      ul: ({ children }) => <ul className="list-disc list-inside my-1">{children}</ul>,
      ol: ({ children }) => <ol className="list-decimal list-inside my-1">{children}</ol>,
      li: ({ children }) => <li className="my-0.5">{children}</li>,
      table: ({ children }) => <table className="my-2 font-mono text-sm">{children}</table>,
      th: ({ children }) => <th className="text-left pr-4">{children}</th>,
      td: ({ children }) => <td className="pr-4">{children}</td>,
    };
  }

  if (mode === 'minimal') {
    return {
      ...baseComponents,
      code: ({ className, children, ...props }) => {
        const match = /language-(\w+)/.exec(className || '');
        const isBlock =
          'node' in props && props.node?.position?.start.line !== props.node?.position?.end.line;

        if (match || isBlock) {
          const code = String(children).replace(/\n$/, '');
          return <CodeBlock code={code} language={match?.[1]} mode="full" className="my-1" />;
        }

        return <InlineCode>{children}</InlineCode>;
      },
      pre: ({ children }) => <>{children}</>,
      p: ({ children }) => <p className="my-2 leading-relaxed">{children}</p>,
      ul: ({ children }) => (
        <ul className="my-2 space-y-1 ps-4 pe-2 list-disc marker:text-muted-foreground">
          {children}
        </ul>
      ),
      ol: ({ children }) => <ol className="my-2 space-y-1 pl-6 list-decimal">{children}</ol>,
      li: ({ children }) => <li>{children}</li>,
      table: ({ children }) => (
        <div className="my-3 overflow-x-auto">
          <table className="min-w-full text-sm">{children}</table>
        </div>
      ),
      thead: ({ children }) => <thead className="border-b">{children}</thead>,
      th: ({ children }) => (
        <th className="text-left py-2 px-3 font-semibold text-muted-foreground">{children}</th>
      ),
      td: ({ children }) => <td className="py-2 px-3 border-b border-border/50">{children}</td>,
      h1: ({ children }) => <h1 className="font-sans text-base font-bold mt-5 mb-3">{children}</h1>,
      h2: ({ children }) => (
        <h2 className="font-sans text-base font-semibold mt-4 mb-3">{children}</h2>
      ),
      h3: ({ children }) => (
        <h3 className="font-sans text-sm font-semibold mt-4 mb-2">{children}</h3>
      ),
      blockquote: ({ children }) => (
        <blockquote className="border-l-2 border-muted-foreground/30 pl-3 my-2 text-muted-foreground italic">
          {children}
        </blockquote>
      ),
      hr: () => <hr className="my-4 border-border" />,
      strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
      em: ({ children }) => <em className="italic">{children}</em>,
    };
  }

  return {
    ...baseComponents,
    code: ({ className, children, ...props }) => {
      const match = /language-(\w+)/.exec(className || '');
      const isBlock =
        'node' in props && props.node?.position?.start.line !== props.node?.position?.end.line;

      if (match || isBlock) {
        const code = String(children).replace(/\n$/, '');
        return <CodeBlock code={code} language={match?.[1]} mode="full" className="my-1" />;
      }

      return <InlineCode>{children}</InlineCode>;
    },
    pre: ({ children }) => <>{children}</>,
    p: ({ children }) => <p className="my-3 leading-relaxed">{children}</p>,
    ul: ({ children }) => (
      <ul className="my-3 space-y-1.5 ps-4 pe-2 list-disc marker:text-muted-foreground">
        {children}
      </ul>
    ),
    ol: ({ children }) => <ol className="my-3 space-y-1.5 pl-6 list-decimal">{children}</ol>,
    li: ({ children }) => <li className="leading-relaxed">{children}</li>,
    table: ({ children }) => (
      <div className="my-4 overflow-x-auto rounded-md border">
        <table className="min-w-full divide-y divide-border">{children}</table>
      </div>
    ),
    thead: ({ children }) => <thead className="bg-muted/50">{children}</thead>,
    tbody: ({ children }) => <tbody className="divide-y divide-border">{children}</tbody>,
    th: ({ children }) => <th className="text-left py-3 px-4 font-semibold text-sm">{children}</th>,
    td: ({ children }) => <td className="py-3 px-4 text-sm">{children}</td>,
    tr: ({ children }) => <tr className="hover:bg-muted/30 transition-colors">{children}</tr>,
    h1: ({ children }) => <h1 className="font-sans text-base font-bold mt-7 mb-4">{children}</h1>,
    h2: ({ children }) => (
      <h2 className="font-sans text-base font-semibold mt-6 mb-3">{children}</h2>
    ),
    h3: ({ children }) => <h3 className="font-sans text-sm font-semibold mt-5 mb-3">{children}</h3>,
    h4: ({ children }) => <h4 className="text-sm font-semibold mt-3 mb-1">{children}</h4>,
    blockquote: ({ children }) => (
      <blockquote className="border-l-4 border-foreground/30 bg-muted/30 pl-4 pr-3 py-2 my-3 rounded-r-md">
        {children}
      </blockquote>
    ),
    input: ({ type, checked }) => {
      if (type === 'checkbox') {
        return (
          <input
            type="checkbox"
            checked={checked}
            readOnly
            className="mr-2 rounded border-muted-foreground"
          />
        );
      }
      return <input type={type} />;
    },
    hr: () => <hr className="my-6 border-border" />,
    strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
    em: ({ children }) => <em className="italic">{children}</em>,
    del: ({ children }) => <del className="line-through text-muted-foreground">{children}</del>,
  };
}

export function Markdown({
  children,
  mode = 'minimal',
  className,
  onUrlClick,
  onFileClick,
  renderMention,
  renderImage,
  renderFileCard,
  cdnDomain,
  autolinkIssueIdentifiers,
}: MarkdownProps): React.JSX.Element {
  useMarkdownImagePolicyVersion();

  const components = React.useMemo(
    () =>
      createComponents(mode, onUrlClick, onFileClick, renderMention, renderImage, renderFileCard),
    [mode, onUrlClick, onFileClick, renderMention, renderImage, renderFileCard],
  );

  const processedContent = React.useMemo(() => {
    let result = preprocessMentionShortcodes(children);
    if (autolinkIssueIdentifiers) result = preprocessIssueIdentifiers(result);
    result = preprocessLinks(result);
    result = preprocessFileCards(result, cdnDomain ?? '');
    return result;
  }, [children, cdnDomain, autolinkIssueIdentifiers]);

  return (
    <div className={cn('markdown-content break-words', className)}>
      <ReactMarkdown
        remarkPlugins={[
          [remarkMath, { singleDollarTextMath: false }],
          remarkBreaks,
          [remarkGfm, { singleTilde: false }],
        ]}
        rehypePlugins={[rehypeRaw, [rehypeSanitize, markdownSanitizeSchema], rehypeKatex]}
        urlTransform={markdownUrlTransform}
        components={components}
      >
        {processedContent}
      </ReactMarkdown>
    </div>
  );
}

export const MemoizedMarkdown = React.memo(Markdown, (prevProps, nextProps) => {
  if (prevProps.id && nextProps.id) {
    return (
      prevProps.id === nextProps.id &&
      prevProps.children === nextProps.children &&
      prevProps.mode === nextProps.mode
    );
  }
  return prevProps.children === nextProps.children && prevProps.mode === nextProps.mode;
});
MemoizedMarkdown.displayName = 'MemoizedMarkdown';

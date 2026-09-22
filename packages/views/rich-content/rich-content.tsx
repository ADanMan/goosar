'use client';

import { createContext, isValidElement, memo, useContext, useMemo, useRef } from 'react';
import ReactMarkdown, {
  type Components,
  type ExtraProps,
  type Options as ReactMarkdownOptions,
} from 'react-markdown';
import type { ComponentPropsWithoutRef, ReactNode } from 'react';
import rehypeKatex from 'rehype-katex';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import rehypeRaw from 'rehype-raw';
import rehypeSanitize from 'rehype-sanitize';
import { cn } from '@goosar/ui/lib/utils';
import { useWorkspacePaths, useWorkspaceSlug } from '@goosar/core/paths';
import { useConfigStore } from '@goosar/core/config';
import type { Attachment } from '@goosar/core/types';
import {
  BlockedImagePlaceholder,
  getMarkdownImagePolicy,
  isAllowedFileCardHref,
  isIssueIdentifier,
  markdownSanitizeSchema,
  markdownUrlTransform,
} from '@goosar/ui/markdown';
import { useSyncMarkdownImagePolicy } from './use-markdown-image-policy';
import { useSyncBlockedImageLabel } from './use-blocked-image-label';
import { AppLink, useAppOrigin } from '../navigation';
import { IssueMentionCard } from '../issues/components/issue-mention-card';
import { useResolveIssueIdentifier } from '../issues/hooks';
import { ProjectChip } from '../projects/components/project-chip';
import { useLinkHover, LinkHoverCard } from '../editor/link-hover-card';
import { openLink, isMentionHref } from '../editor/utils/link-handler';
import { preprocessMarkdown } from '../editor/utils/preprocess';
import { highlightToHtml } from '../editor/utils/highlight-markdown';
import { AttachmentDownloadProvider } from '../editor/attachment-download-context';
import { Attachment as AttachmentRenderer } from '../editor/attachment';
import { computeClosedFenceOffsets } from './streaming-fence';
import {
  CodeBlockShell,
  RichFenceBlock,
  StaticCodeBody,
  isRichFenceLanguage,
  shouldUpgradeFence,
} from './rich-code-block';
import 'katex/dist/katex.min.css';
import '../editor/styles/index.css';
import './rich-content.css';

export type RichContentDensity = 'compact' | 'document';
export type RichContentPhase = 'streaming' | 'settled';

const ClosedFenceContext = createContext<ReadonlySet<number>>(new Set<number>());

function useIsFenceClosed(offset: number | undefined): boolean {
  const closed = useContext(ClosedFenceContext);
  return offset != null && closed.has(offset);
}

function IssueMentionLink({ issueId, label }: { issueId: string; label?: string }) {
  return (
    <span className="inline align-middle" onClick={(e) => e.stopPropagation()}>
      <IssueMentionCard issueId={issueId} fallbackLabel={label} />
    </span>
  );
}

function AutolinkedIssueMentionLink({ identifier }: { identifier: string }) {
  const issue = useResolveIssueIdentifier(identifier);
  if (!issue) return <>{identifier}</>;
  return <IssueMentionLink issueId={issue.id} label={identifier} />;
}

function ProjectMentionLink({ projectId, label }: { projectId: string; label?: string }) {
  const p = useWorkspacePaths();
  return (
    <span className="inline align-middle" onClick={(e) => e.stopPropagation()}>
      <AppLink
        href={p.projectDetail(projectId)}
        newTabTitle={label}
        className="project-mention not-prose inline-flex"
      >
        <ProjectChip
          projectId={projectId}
          fallbackLabel={label}
          className="cursor-pointer hover:bg-accent transition-colors"
        />
      </AppLink>
    </span>
  );
}

function childrenToLabel(children: ReactNode): string | undefined {
  if (typeof children === 'string') return children;
  if (Array.isArray(children)) return children.join('');
  return undefined;
}

function RichLink({ href, children }: { href?: string; children?: ReactNode }) {
  const slug = useWorkspaceSlug();
  const appOrigin = useAppOrigin();

  if (href?.startsWith('slash://skill/')) {
    return <span className="slash-command">{children}</span>;
  }

  if (isMentionHref(href)) {
    const match = href.match(/^mention:\/\/(member|agent|issue|project|all)\/(.+)$/);
    if (match?.[1] === 'issue' && match[2]) {
      if (isIssueIdentifier(match[2])) {
        return <AutolinkedIssueMentionLink identifier={match[2]} />;
      }
      return <IssueMentionLink issueId={match[2]} label={childrenToLabel(children)} />;
    }
    if (match?.[1] === 'project' && match[2]) {
      return <ProjectMentionLink projectId={match[2]} label={childrenToLabel(children)} />;
    }
    return <span className="mention">{children}</span>;
  }

  return (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        if (href) openLink(href, slug, appOrigin);
      }}
    >
      {children}
    </a>
  );
}

function getTextContent(node: ReactNode): string {
  if (node == null || typeof node === 'boolean') return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(getTextContent).join('');
  if (isValidElement<{ children?: ReactNode }>(node)) {
    return getTextContent(node.props.children);
  }
  return '';
}

type RichCodeProps = ComponentPropsWithoutRef<'code'> & ExtraProps;
type RichPreProps = ComponentPropsWithoutRef<'pre'> & ExtraProps;

function nodeStartOffset(node: ExtraProps['node']): number | undefined {
  return node?.position?.start.offset;
}

function stringProperty(node: ExtraProps['node'], key: string): string {
  const value = node?.properties?.[key];
  return typeof value === 'string' ? value : '';
}

function isMultiLineNode(node: ExtraProps['node']): boolean {
  const position = node?.position;
  return position != null && position.start.line !== position.end.line;
}

function RichCode({ className, children, node, ...props }: RichCodeProps) {
  const language = /language-(\w+)/.exec(className || '')?.[1];
  const isBlock = isMultiLineNode(node);
  const isFenceClosed = useIsFenceClosed(nodeStartOffset(node));

  if (isBlock && shouldUpgradeFence(language, isFenceClosed)) {
    if (isRichFenceLanguage(language)) {
      return <RichFenceBlock language={language} body={String(children).replace(/\n$/, '')} />;
    }
  }

  if (!isBlock && !language) {
    return <code {...props}>{children}</code>;
  }

  return <StaticCodeBody language={language} body={String(children)} />;
}

function readFencedCodeChild(children: ReactNode): {
  language?: string;
  offset?: number;
} {
  const child = Array.isArray(children) ? children[0] : children;
  if (!isValidElement<{ className?: string } & ExtraProps>(child)) return {};
  return {
    language: /(?:^|\s)language-(\w+)(?:\s|$)/.exec(child.props.className ?? '')?.[1],
    offset: nodeStartOffset(child.props.node),
  };
}

function RichPre({ children }: RichPreProps) {
  const { language, offset } = readFencedCodeChild(children);
  const isFenceClosed = useIsFenceClosed(offset);

  if (shouldUpgradeFence(language, isFenceClosed)) {
    return <>{children}</>;
  }

  return (
    <CodeBlockShell language={language} code={getTextContent(children).replace(/\n$/, '')}>
      {children}
    </CodeBlockShell>
  );
}

const COMPONENTS: Partial<Components> = {
  a: RichLink,

  img: ({ src, alt }) => {
    const url = typeof src === 'string' ? src : '';
    if (url === '' && getMarkdownImagePolicy().mode !== 'allow') {
      return <BlockedImagePlaceholder alt={alt ?? ''} />;
    }
    return (
      <AttachmentRenderer
        attachment={{
          kind: 'url',
          url,
          filename: alt ?? '',
          forceKind: 'image',
        }}
      />
    );
  },

  div: ({ node, children, ...props }) => {
    if (stringProperty(node, 'dataType') === 'fileCard') {
      const rawHref = stringProperty(node, 'dataHref');
      const href = isAllowedFileCardHref(rawHref) ? rawHref : '';
      return (
        <AttachmentRenderer
          attachment={{
            kind: 'url',
            url: href,
            filename: stringProperty(node, 'dataFilename'),
          }}
        />
      );
    }
    return <div {...props}>{children}</div>;
  },

  table: ({ children }) => (
    <div className="tableWrapper">
      <table>{children}</table>
    </div>
  ),

  code: RichCode,
  pre: RichPre,
};

const REMARK_PLUGINS = [
  [remarkMath, { singleDollarTextMath: false }],
  remarkBreaks,
  [remarkGfm, { singleTilde: false }],
] satisfies NonNullable<ReactMarkdownOptions['remarkPlugins']>;

const REHYPE_PLUGINS = [
  rehypeRaw,
  [rehypeSanitize, markdownSanitizeSchema],
  rehypeKatex,
] satisfies NonNullable<ReactMarkdownOptions['rehypePlugins']>;

export interface RichContentProps {
  content: string;
  attachments?: Attachment[];
  density?: RichContentDensity;
  phase?: RichContentPhase;
  className?: string;
}

export const RichContent = memo(function RichContent({
  content,
  attachments,
  density = 'document',
  phase = 'settled',
  className,
}: RichContentProps) {
  const cdnDomain = useConfigStore((s) => s.cdnDomain);

  const imagePolicyVersion = useSyncMarkdownImagePolicy();

  useSyncBlockedImageLabel();

  const processed = useMemo(
    () =>
      highlightToHtml(preprocessMarkdown(content, { cdnDomain, autolinkIssueIdentifiers: true })),
    [content, cdnDomain],
  );

  const closedFences = useMemo(() => computeClosedFenceOffsets(processed), [processed]);

  const wrapperRef = useRef<HTMLDivElement>(null);
  const hover = useLinkHover(wrapperRef);

  const markdown = useMemo(() => {
    void imagePolicyVersion;
    return (
      <ClosedFenceContext.Provider value={closedFences}>
        <ReactMarkdown
          remarkPlugins={REMARK_PLUGINS}
          rehypePlugins={REHYPE_PLUGINS}
          urlTransform={markdownUrlTransform}
          components={COMPONENTS}
        >
          {processed}
        </ReactMarkdown>
      </ClosedFenceContext.Provider>
    );
  }, [processed, closedFences, imagePolicyVersion]);

  return (
    <AttachmentDownloadProvider attachments={attachments}>
      <div
        ref={wrapperRef}
        data-rich-content=""
        data-density={density}
        data-phase={phase}
        className={cn(
          'rich-text-editor readonly text-sm',
          density === 'compact' && 'rich-content-compact',
          className,
        )}
      >
        {markdown}
        <LinkHoverCard {...hover} />
      </div>
    </AttachmentDownloadProvider>
  );
});

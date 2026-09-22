import * as React from 'react';
import { Markdown, type RenderMode } from './Markdown';

export interface StreamingMarkdownProps {
  content: string;
  isStreaming: boolean;
  mode?: RenderMode;
  className?: string;
  onUrlClick?: (url: string) => void;
  onFileClick?: (path: string) => void;
  renderMention?: (props: { type: string; id: string }) => React.ReactNode;
  cdnDomain?: string;
}

interface Block {
  content: string;
  isCodeBlock: boolean;
}

function simpleHash(str: string): string {
  let hash = 5381;
  for (let i = 0; i < str.length; i++) {
    hash = ((hash << 5) + hash) ^ str.charCodeAt(i);
  }
  return (hash >>> 0).toString(36);
}

function splitIntoBlocks(content: string): Block[] {
  const blocks: Block[] = [];
  const lines = content.split('\n');
  let currentBlock = '';
  let inCodeBlock = false;
  let inMathBlock = false;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? '';

    if (line.startsWith('```')) {
      if (!inCodeBlock) {
        if (currentBlock.trim()) {
          blocks.push({ content: currentBlock.trim(), isCodeBlock: false });
          currentBlock = '';
        }
        inCodeBlock = true;
        currentBlock = line + '\n';
      } else {
        currentBlock += line;
        blocks.push({ content: currentBlock, isCodeBlock: true });
        currentBlock = '';
        inCodeBlock = false;
      }
    } else if (inCodeBlock) {
      currentBlock += line + '\n';
      // Check for display math fence ($$)
    } else if (line.trim() === '$$') {
      if (!inMathBlock) {
        if (currentBlock.trim()) {
          blocks.push({ content: currentBlock.trim(), isCodeBlock: false });
          currentBlock = '';
        }
        inMathBlock = true;
        currentBlock = line + '\n';
      } else {
        currentBlock += line;
        blocks.push({ content: currentBlock, isCodeBlock: false });
        currentBlock = '';
        inMathBlock = false;
      }
    } else if (inMathBlock) {
      currentBlock += line + '\n';
    } else if (line === '') {
      if (currentBlock.trim()) {
        blocks.push({ content: currentBlock.trim(), isCodeBlock: false });
        currentBlock = '';
      }
    } else {
      if (currentBlock) {
        currentBlock += '\n' + line;
      } else {
        currentBlock = line;
      }
    }
  }

  if (currentBlock) {
    blocks.push({
      content: inCodeBlock || inMathBlock ? currentBlock : currentBlock.trim(),
      isCodeBlock: inCodeBlock,
    });
  }

  return blocks;
}

const MemoizedBlock = React.memo(
  function Block({
    content,
    mode,
    className,
    onUrlClick,
    onFileClick,
    renderMention,
    cdnDomain,
  }: {
    content: string;
    mode: RenderMode;
    className?: string;
    onUrlClick?: (url: string) => void;
    onFileClick?: (path: string) => void;
    renderMention?: (props: { type: string; id: string }) => React.ReactNode;
    cdnDomain?: string;
  }) {
    return (
      <Markdown
        mode={mode}
        className={className}
        onUrlClick={onUrlClick}
        onFileClick={onFileClick}
        renderMention={renderMention}
        cdnDomain={cdnDomain}
      >
        {content}
      </Markdown>
    );
  },
  (prev, next) => {
    return (
      prev.content === next.content && prev.mode === next.mode && prev.className === next.className
    );
  },
);
MemoizedBlock.displayName = 'MemoizedBlock';

export function StreamingMarkdown({
  content,
  isStreaming,
  mode = 'minimal',
  className,
  onUrlClick,
  onFileClick,
  renderMention,
  cdnDomain,
}: StreamingMarkdownProps): React.JSX.Element {
  const blocks = React.useMemo(
    () => (isStreaming ? splitIntoBlocks(content) : []),
    [content, isStreaming],
  );

  if (!isStreaming) {
    return (
      <Markdown
        mode={mode}
        className={className}
        onUrlClick={onUrlClick}
        onFileClick={onFileClick}
        renderMention={renderMention}
        cdnDomain={cdnDomain}
      >
        {content}
      </Markdown>
    );
  }

  if (blocks.length === 0) {
    return <></>;
  }

  return (
    <>
      {blocks.map((block, i) => {
        const isLastBlock = i === blocks.length - 1;

        const key = isLastBlock ? `active-${i}` : `block-${i}-${simpleHash(block.content)}`;

        return (
          <MemoizedBlock
            key={key}
            content={block.content}
            mode={mode}
            className={className}
            onUrlClick={onUrlClick}
            onFileClick={onFileClick}
            renderMention={renderMention}
            cdnDomain={cdnDomain}
          />
        );
      })}
    </>
  );
}

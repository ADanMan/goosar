'use client';

import { useMemo } from 'react';
import { toHtml } from 'hast-util-to-html';
import { cn } from '@goosar/ui/lib/utils';
import { highlightCode } from './syntax-highlight';
import './styles/code.css';

interface CodeBlockStaticProps {
  language: string | undefined;
  body: string;
  className?: string;
}

export function CodeBlockStatic({ language, body, className }: CodeBlockStaticProps) {
  const html = useMemo(() => {
    const code = body.replace(/\n$/, '');
    try {
      const tree = highlightCode(code, language);
      return toHtml(tree) as string;
    } catch {
      return escapeHtml(code);
    }
  }, [body, language]);

  return (
    <pre className={cn('rich-text-editor m-0 overflow-auto text-sm', className)}>
      <code
        className={cn('hljs', language && `language-${language}`)}
        dangerouslySetInnerHTML={{ __html: html }}
      />
    </pre>
  );
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

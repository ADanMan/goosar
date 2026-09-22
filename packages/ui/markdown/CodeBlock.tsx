import * as React from 'react';
import { codeToHtml, bundledLanguages, type BundledLanguage } from 'shiki';
import { Copy, Check } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@goosar/ui/components/ui/button';
import { Tooltip, TooltipTrigger, TooltipContent } from '@goosar/ui/components/ui/tooltip';
import { cn } from '@goosar/ui/lib/utils';
import { copyText } from '../lib/clipboard';
import { CODE_LIGATURE_CLASS, CODE_LIGATURE_DESCENDANT_CLASS } from '@goosar/ui/lib/code-style';

export interface CodeBlockProps {
  code: string;
  language?: string;
  className?: string;
  mode?: 'terminal' | 'minimal' | 'full';
}

const LANGUAGE_ALIASES: Record<string, BundledLanguage> = {
  js: 'javascript',
  ts: 'typescript',
  py: 'python',
  sh: 'bash',
  zsh: 'bash',
  yml: 'yaml',
  rb: 'ruby',
  rs: 'rust',
  kt: 'kotlin',
  'objective-c': 'objc',
  objc: 'objc',
};

const highlightCache = new Map<string, string>();
const CACHE_MAX_SIZE = 200;

function getCacheKey(code: string, lang: string): string {
  return `${lang}:${code}`;
}

function isValidLanguage(lang: string): lang is BundledLanguage {
  const normalized = LANGUAGE_ALIASES[lang] || lang;
  return normalized in bundledLanguages;
}

export function CodeBlock({
  code,
  language = 'text',
  className,
  mode = 'full',
}: CodeBlockProps): React.JSX.Element {
  const { t } = useTranslation('ui');
  const [highlighted, setHighlighted] = React.useState<string | null>(null);
  const [isLoading, setIsLoading] = React.useState(true);
  const [copied, setCopied] = React.useState(false);

  const langLower = language.toLowerCase();
  const resolvedLang: string = LANGUAGE_ALIASES[langLower] || langLower;

  React.useEffect(() => {
    let cancelled = false;

    async function highlight(): Promise<void> {
      const cacheKey = getCacheKey(code, resolvedLang);

      const cached = highlightCache.get(cacheKey);
      if (cached) {
        if (!cancelled) {
          setHighlighted(cached);
          setIsLoading(false);
        }
        return;
      }

      try {
        const lang = isValidLanguage(resolvedLang) ? resolvedLang : 'text';

        const html = await codeToHtml(code, {
          lang,
          themes: {
            light: 'github-light',
            dark: 'github-dark',
          },
          defaultColor: false,
        });

        if (highlightCache.size >= CACHE_MAX_SIZE) {
          const firstKey = highlightCache.keys().next().value;
          if (firstKey) highlightCache.delete(firstKey);
        }
        highlightCache.set(cacheKey, html);

        if (!cancelled) {
          setHighlighted(html);
          setIsLoading(false);
        }
      } catch (error) {
        console.warn(`Shiki highlighting failed for language "${resolvedLang}":`, error);
        if (!cancelled) {
          setHighlighted(null);
          setIsLoading(false);
        }
      }
    }

    highlight();

    return () => {
      cancelled = true;
    };
  }, [code, resolvedLang]);

  const handleCopy = React.useCallback(async () => {
    if (await copyText(code)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  }, [code]);

  if (mode === 'terminal') {
    return (
      <pre className={cn('font-mono text-sm whitespace-pre-wrap', CODE_LIGATURE_CLASS, className)}>
        <code className={cn('font-mono', CODE_LIGATURE_CLASS)}>{code}</code>
      </pre>
    );
  }

  if (mode === 'minimal') {
    if (isLoading || !highlighted) {
      return (
        <pre
          className={cn('font-mono text-sm whitespace-pre-wrap', CODE_LIGATURE_CLASS, className)}
        >
          <code className={cn('font-mono', CODE_LIGATURE_CLASS)}>{code}</code>
        </pre>
      );
    }

    return (
      <div
        className={cn(
          'font-mono text-sm [&_pre]:!bg-transparent [&_pre]:!p-0 [&_pre]:whitespace-pre-wrap [&_pre]:break-all [&_code]:!bg-transparent [&_code]:font-mono [&_pre]:font-mono',
          CODE_LIGATURE_CLASS,
          CODE_LIGATURE_DESCENDANT_CLASS,
          className,
        )}
        dangerouslySetInnerHTML={{ __html: highlighted }}
      />
    );
  }

  return (
    <div
      className={cn(
        'relative group rounded-lg overflow-hidden border bg-muted/30 mb-4 last:mb-0',
        className,
      )}
    >
      {/* Language label + copy button */}
      <div className="flex items-center justify-between px-3 py-1.5 bg-muted/50 border-b text-xs">
        <span className="text-muted-foreground font-medium uppercase tracking-wide">
          {resolvedLang !== 'text' ? resolvedLang : t(($) => $.plain_text)}
        </span>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={handleCopy}
                className="opacity-0 group-hover:opacity-100 transition-opacity text-muted-foreground hover:text-foreground"
                aria-label={t(($) => $.copy_code)}
              >
                {copied ? (
                  <Check className="size-3.5 text-success" />
                ) : (
                  <Copy className="size-3.5" />
                )}
              </Button>
            }
          />
          <TooltipContent>{t(($) => $.copy_code)}</TooltipContent>
        </Tooltip>
      </div>

      {/* Code content */}
      <div className="p-3 overflow-x-auto">
        {isLoading || !highlighted ? (
          <pre
            className={cn('font-mono text-sm whitespace-pre-wrap break-all', CODE_LIGATURE_CLASS)}
          >
            <code className={cn('font-mono', CODE_LIGATURE_CLASS)}>{code}</code>
          </pre>
        ) : (
          <div
            className={cn(
              'font-mono text-sm [&_pre]:!bg-transparent [&_pre]:!m-0 [&_pre]:!p-0 [&_pre]:whitespace-pre-wrap [&_pre]:break-all [&_code]:!bg-transparent [&_code]:font-mono [&_pre]:font-mono',
              CODE_LIGATURE_CLASS,
              CODE_LIGATURE_DESCENDANT_CLASS,
            )}
            dangerouslySetInnerHTML={{ __html: highlighted }}
          />
        )}
      </div>
    </div>
  );
}

export function InlineCode({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): React.JSX.Element {
  return (
    <code
      className={cn(
        'px-1.5 py-0.5 rounded bg-foreground/[0.03] border border-foreground/[0.05] font-mono text-sm text-foreground/75',
        CODE_LIGATURE_CLASS,
        className,
      )}
    >
      {children}
    </code>
  );
}

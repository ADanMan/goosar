// Публичный Markdown-компонент мобилки: гибридный рендерер — проза через
// нативный enriched-markdown, блоки кода через собственный компонент.
import { useCallback, useMemo } from 'react';
import { Linking, View } from 'react-native';
import { router } from 'expo-router';
import { EnrichedMarkdownText } from 'react-native-enriched-markdown';
import type { Attachment } from '@goosar/core/types';
import { useWorkspaceStore } from '@/data/workspace-store';
import { preprocessMobileMarkdown } from './preprocess';
import { useMarkdownStyle } from './markdown-style';
import { splitMarkdown } from './split-markdown';
import { CodeBlock } from './code-block';
import { MarkdownImage } from './markdown-image';

interface Props {
  content: string;
  attachments?: Attachment[];
  selectable?: boolean;
  compact?: boolean;
}

export function Markdown({ content, attachments, selectable = true, compact = false }: Props) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const baseStyle = useMarkdownStyle();
  const markdownStyle = useMemo(
    () =>
      compact
        ? {
            ...baseStyle,
            paragraph: {
              ...baseStyle.paragraph,
              marginBottom: 0,
              lineHeight: 20,
            },
          }
        : baseStyle,
    [baseStyle, compact],
  );

  const segments = useMemo(() => {
    const processed = preprocessMobileMarkdown(content);
    return splitMarkdown(processed);
  }, [content]);

  const onLinkPress = useCallback(
    ({ url }: { url: string }) => {
      if (url.startsWith('mention://')) {
        const rest = url.slice('mention://'.length);
        const slash = rest.indexOf('/');
        if (slash < 0) return;
        const type = rest.slice(0, slash);
        const id = rest.slice(slash + 1);
        if (type === 'issue' && id && wsSlug) {
          router.push(`/${wsSlug}/issue/${id}`);
        }
        return;
      }
      Linking.openURL(url).catch(() => {
        // Silent: failing loudly is worse than a no-op tap.
      });
    },
    [wsSlug],
  );

  if (segments.length === 0) return null;

  return (
    <View className="gap-3">
      {segments.map((seg, i) => {
        switch (seg.type) {
          case 'prose':
            return (
              <EnrichedMarkdownText
                key={i}
                flavor="github"
                markdown={seg.content}
                markdownStyle={markdownStyle}
                onLinkPress={onLinkPress}
                selectable={selectable}
              />
            );
          case 'code':
            return <CodeBlock key={i} code={seg.code} lang={seg.lang} selectable={selectable} />;
          case 'image':
            return <MarkdownImage key={i} uri={seg.uri} alt={seg.alt} attachments={attachments} />;
        }
      })}
    </View>
  );
}

/**
 * Блок кода в markdown: заголовок с меткой языка и кнопкой копирования,
 * ниже — сам код с горизонтальным скроллом.
 *
 * Два пути рендера: подсветка токенов через Shiki, когда движок и язык
 * известны, либо простой текст в остальных случаях (неизвестный язык,
 * движок ещё не готов).
 *
 * Кнопка копирования видна постоянно (без hover на тач-устройствах);
 * по тапу копирует код в буфер, даёт хаптик-отклик и на 2 секунды
 * показывает галочку — iOS не показывает системное уведомление о копировании.
 */
import { useEffect, useRef, useState } from 'react';
import { Pressable, ScrollView, View } from 'react-native';
import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import Svg, { Path, Rect } from 'react-native-svg';
import { Text } from '@/components/ui/text';
import { THEME } from '@/lib/theme';
import { useColorScheme } from '@/lib/use-color-scheme';
import {
  CODE_BLOCK_CONTAINER_CLASS,
  CODE_BLOCK_LANG_LABEL_CLASS,
  CODE_BLOCK_TEXT_CLASS,
} from './tokens';
import {
  highlight,
  resolveLang,
  SHIKI_THEME_DARK,
  SHIKI_THEME_LIGHT,
  type HighlightedLine,
} from './shiki';

interface Props {
  code: string;
  lang?: string;
  selectable?: boolean;
}

export function CodeBlock({ code, lang, selectable = true }: Props) {
  const { isDarkColorScheme } = useColorScheme();
  const theme = isDarkColorScheme ? SHIKI_THEME_DARK : SHIKI_THEME_LIGHT;
  const resolvedLang = resolveLang(lang);
  const [lines, setLines] = useState<HighlightedLine[] | null>(null);

  useEffect(() => {
    if (!resolvedLang) {
      setLines(null);
      return;
    }
    let cancelled = false;
    void highlight(code, resolvedLang, theme).then((result) => {
      if (!cancelled) setLines(result);
    });
    return () => {
      cancelled = true;
    };
  }, [code, resolvedLang, theme]);

  return (
    <View className={CODE_BLOCK_CONTAINER_CLASS}>
      <CodeBlockHeader code={code} lang={lang} />
      <ScrollView horizontal showsHorizontalScrollIndicator={false}>
        {lines ? (
          <HighlightedCode lines={lines} selectable={selectable} />
        ) : (
          <PlainCode code={code} selectable={selectable} />
        )}
      </ScrollView>
    </View>
  );
}

function PlainCode({ code, selectable }: { code: string; selectable: boolean }) {
  return (
    <Text className={CODE_BLOCK_TEXT_CLASS} selectable={selectable}>
      {code}
    </Text>
  );
}

function HighlightedCode({ lines, selectable }: { lines: HighlightedLine[]; selectable: boolean }) {
  return (
    <View>
      {lines.map((line, i) => (
        <Text key={i} className={CODE_BLOCK_TEXT_CLASS} selectable={selectable}>
          {line.tokens.length === 0
            ? ' '
            : line.tokens.map((t, j) => (
                <Text key={j} style={t.color ? { color: t.color } : undefined}>
                  {t.content}
                </Text>
              ))}
        </Text>
      ))}
    </View>
  );
}

function CodeBlockHeader({ code, lang }: Props) {
  const { isDarkColorScheme } = useColorScheme();
  const t = isDarkColorScheme ? THEME.dark : THEME.light;
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  const onCopy = async () => {
    try {
      await Clipboard.setStringAsync(code);
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
      setCopied(true);
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard write failed (extremely rare on iOS). Silent — no
      // recovery path beats a confusing toast.
    }
  };

  return (
    <View className="flex-row items-center justify-between mb-1">
      {lang ? (
        <Text className={`${CODE_BLOCK_LANG_LABEL_CLASS} flex-1 mr-2`} numberOfLines={1}>
          {lang}
        </Text>
      ) : (
        <View className="flex-1" />
      )}
      <Pressable
        onPress={onCopy}
        hitSlop={8}
        accessibilityRole="button"
        accessibilityLabel={copied ? 'Code copied' : 'Copy code'}
      >
        {copied ? <CheckIcon color={t.success} /> : <CopyIcon color={t.mutedForeground} />}
      </Pressable>
    </View>
  );
}

function CopyIcon({ color }: { color: string }) {
  return (
    <Svg width={14} height={14} viewBox="0 0 16 16" fill="none">
      <Rect x={5} y={5} width={9} height={9} rx={1.5} stroke={color} strokeWidth={1.4} />
      <Path
        d="M11 4.5V3.5A1.5 1.5 0 0 0 9.5 2H3.5A1.5 1.5 0 0 0 2 3.5v6A1.5 1.5 0 0 0 3.5 11h1"
        stroke={color}
        strokeWidth={1.4}
        strokeLinecap="round"
      />
    </Svg>
  );
}

function CheckIcon({ color }: { color: string }) {
  return (
    <Svg width={14} height={14} viewBox="0 0 16 16" fill="none">
      <Path
        d="M3.5 8.5L6.5 11.5L12.5 5"
        stroke={color}
        strokeWidth={1.6}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </Svg>
  );
}

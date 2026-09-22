// Общее состояние ввода с @упоминаниями для любого RN TextInput: текст,
// выделение, маркеры, тулбар markdown.
import { useCallback, useMemo, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import type { NativeSyntheticEvent, TextInputSelectionChangeEventData } from 'react-native';
import {
  insertMention,
  serializeMentions,
  tokenAtCursor,
  type MentionMarker,
} from '@/lib/mention-serialize';

export interface MentioningState {
  start: number;
  query: string;
}

export interface MentionInputSnapshot {
  text: string;
  markers: MentionMarker[];
  selection: { start: number; end: number };
}

export interface UseMentionInputReturn {
  text: string;
  setText: Dispatch<SetStateAction<string>>;
  selection: { start: number; end: number };
  setSelection: (sel: { start: number; end: number }) => void;
  markers: MentionMarker[];
  mentioning: MentioningState | null;
  handlers: {
    onChangeText: (next: string) => void;
    onSelectionChange: (e: NativeSyntheticEvent<TextInputSelectionChangeEventData>) => void;
    onAtButtonPress: () => void;
  };
  suggestionBar: {
    visible: boolean;
    query: string;
    onSelect: (mention: MentionMarker) => void;
  };
  insertAtCursor: (text: string, cursorOffsetFromEnd?: number) => void;
  insertAtLineStart: (prefix: string) => void;
  serialize: () => string;
  snapshot: () => MentionInputSnapshot;
  restore: (snap: MentionInputSnapshot) => void;
  reset: () => void;
}

export function useMentionInput(): UseMentionInputReturn {
  const [text, setText] = useState('');
  const [selection, setSelection] = useState<{ start: number; end: number }>({
    start: 0,
    end: 0,
  });
  const [markers, setMarkers] = useState<MentionMarker[]>([]);
  const [mentioning, setMentioning] = useState<MentioningState | null>(null);

  const textRef = useRef(text);
  const selectionRef = useRef(selection);
  textRef.current = text;
  selectionRef.current = selection;

  const recomputeMentioning = useCallback((nextText: string, cursor: number) => {
    const token = tokenAtCursor(nextText, cursor);
    setMentioning(token ? { start: token.start, query: token.query } : null);
  }, []);

  const onChangeText = useCallback(
    (next: string) => {
      textRef.current = next;
      setText(next);
      recomputeMentioning(next, selectionRef.current.end);
    },
    [recomputeMentioning],
  );

  const onSelectionChange = useCallback(
    (e: NativeSyntheticEvent<TextInputSelectionChangeEventData>) => {
      const sel = e.nativeEvent.selection;
      selectionRef.current = sel;
      setSelection(sel);
      recomputeMentioning(textRef.current, sel.end);
    },
    [recomputeMentioning],
  );

  const onAtButtonPress = useCallback(() => {
    const t = textRef.current;
    const s = selectionRef.current;
    const before = t.slice(0, s.start);
    const after = t.slice(s.end);
    const needsPad = before.length > 0 && !/\s$/.test(before);
    const inserted = (needsPad ? ' ' : '') + '@';
    const next = before + inserted + after;
    const cursor = before.length + inserted.length;
    textRef.current = next;
    selectionRef.current = { start: cursor, end: cursor };
    setText(next);
    setSelection({ start: cursor, end: cursor });
    recomputeMentioning(next, cursor);
  }, [recomputeMentioning]);

  const onSelectMention = useCallback(
    (mention: MentionMarker) => {
      if (!mentioning) return;
      const { newText, newSelection, marker } = insertMention(
        textRef.current,
        { start: mentioning.start, queryLength: mentioning.query.length },
        mention,
      );
      textRef.current = newText;
      selectionRef.current = newSelection;
      setText(newText);
      setSelection(newSelection);
      setMarkers((prev) => [...prev, marker]);
      setMentioning(null);
    },
    [mentioning],
  );

  const insertAtCursor = useCallback((insert: string, cursorOffsetFromEnd = 0) => {
    const t = textRef.current;
    const s = selectionRef.current;
    const before = t.slice(0, s.start);
    const after = t.slice(s.end);
    const next = before + insert + after;
    const cursor = before.length + insert.length - cursorOffsetFromEnd;
    textRef.current = next;
    selectionRef.current = { start: cursor, end: cursor };
    setText(next);
    setSelection({ start: cursor, end: cursor });
    setMentioning(null);
  }, []);

  const insertAtLineStart = useCallback((prefix: string) => {
    const t = textRef.current;
    const s = selectionRef.current;
    const before = t.slice(0, s.start);
    const lastNewline = before.lastIndexOf('\n');
    const lineStart = lastNewline === -1 ? 0 : lastNewline + 1;
    const next = t.slice(0, lineStart) + prefix + t.slice(lineStart);
    const cursor = s.end + prefix.length;
    textRef.current = next;
    selectionRef.current = { start: cursor, end: cursor };
    setText(next);
    setSelection({ start: cursor, end: cursor });
    setMentioning(null);
  }, []);

  const serialize = useCallback(() => serializeMentions(text, markers), [text, markers]);

  const snapshot = useCallback(
    (): MentionInputSnapshot => ({ text, markers, selection }),
    [text, markers, selection],
  );

  const restore = useCallback((snap: MentionInputSnapshot) => {
    textRef.current = snap.text;
    selectionRef.current = snap.selection;
    setText(snap.text);
    setMarkers(snap.markers);
    setSelection(snap.selection);
    setMentioning(null);
  }, []);

  const reset = useCallback(() => {
    textRef.current = '';
    selectionRef.current = { start: 0, end: 0 };
    setText('');
    setMarkers([]);
    setSelection({ start: 0, end: 0 });
    setMentioning(null);
  }, []);

  const handlers = useMemo(
    () => ({ onChangeText, onSelectionChange, onAtButtonPress }),
    [onChangeText, onSelectionChange, onAtButtonPress],
  );

  const suggestionBar = useMemo(
    () => ({
      visible: mentioning !== null,
      query: mentioning?.query ?? '',
      onSelect: onSelectMention,
    }),
    [mentioning, onSelectMention],
  );

  return {
    text,
    setText,
    selection,
    setSelection,
    markers,
    mentioning,
    handlers,
    suggestionBar,
    insertAtCursor,
    insertAtLineStart,
    serialize,
    snapshot,
    restore,
    reset,
  };
}

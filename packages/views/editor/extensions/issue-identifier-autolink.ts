// Автоссылки на идентификаторы задач (например MUL-123) в редакторе
// в стиле Linear: срабатывают по границе слова или при вставке.
import { Extension } from '@tiptap/core';
import { Plugin, PluginKey } from '@tiptap/pm/state';
import type { EditorState, Transaction } from '@tiptap/pm/state';
import type { EditorView } from '@tiptap/pm/view';
import type { Mark, Node as PMNode, NodeType } from '@tiptap/pm/model';
import type { RefObject } from 'react';

export interface ResolvedIssueRef {
  id: string;
  identifier: string;
}

export type IssueIdentifierResolver = (identifier: string) => Promise<ResolvedIssueRef | null>;

export interface IssueIdentifierAutolinkOptions {
  resolveRef: RefObject<IssueIdentifierResolver | undefined>;
}

const IDENTIFIER_RE = /(?<![A-Za-z0-9_-])([A-Z][A-Z0-9]*-\d+)(?![A-Za-z0-9_-])/g;
const BOUNDARY_RE = /[A-Za-z0-9_-]/;

const META_REMOVE = 'issueIdentifierAutolinkRemove';

interface Candidate {
  identifier: string;
  from: number;
  to: number;
}

interface PendingCandidate extends Candidate {
  key: number;
}

interface AutolinkPluginState {
  pending: PendingCandidate[];
  seq: number;
}

const pluginKey = new PluginKey<AutolinkPluginState>('issueIdentifierAutolink');

function isSkippedTextNode(marks: readonly Mark[], parent: PMNode | null): boolean {
  if (marks.some((m) => m.type.name === 'code' || m.type.name === 'link')) {
    return true;
  }
  if (parent && parent.type.name === 'codeBlock') return true;
  return false;
}

function collectCandidates(
  state: EditorState,
  rangeFrom = 0,
  rangeTo = Number.POSITIVE_INFINITY,
): Candidate[] {
  const out: Candidate[] = [];
  state.doc.descendants((node, pos, parent) => {
    if (!node.isText || !node.text) return;
    if (isSkippedTextNode(node.marks, parent)) return;
    const text = node.text;
    IDENTIFIER_RE.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = IDENTIFIER_RE.exec(text)) !== null) {
      const identifier = m[1];
      if (!identifier) continue;
      const localEnd = m.index + identifier.length;
      if (localEnd >= text.length) continue; 
      const from = pos + m.index;
      const to = pos + localEnd;
      if (to <= rangeFrom || from >= rangeTo) continue; 
      out.push({ identifier, from, to });
    }
  });
  return out;
}

function changedRange(tr: Transaction): { from: number; to: number } | null {
  let from = Number.POSITIVE_INFINITY;
  let to = -1;
  tr.mapping.maps.forEach((map) => {
    map.forEach((_oldStart, _oldEnd, newStart, newEnd) => {
      from = Math.min(from, newStart);
      to = Math.max(to, newEnd);
    });
  });
  if (to < 0) return null;
  return { from, to };
}

function candidatesFromUserTransaction(tr: Transaction, state: EditorState): Candidate[] {
  const isPaste = tr.getMeta('paste') === true || tr.getMeta('uiEvent') === 'paste';

  if (isPaste) {
    const range = changedRange(tr);
    if (!range) return [];
    return collectCandidates(state, range.from, range.to);
  }

  const caret = state.selection.from;
  const target = collectCandidates(state).find((c) => c.to === caret - 1);
  return target ? [target] : [];
}

function rangeStillPlainIdentifier(
  state: EditorState,
  from: number,
  to: number,
  identifier: string,
): boolean {
  const size = state.doc.content.size;
  if (from < 0 || to > size || from >= to) return false;
  if (state.doc.textBetween(from, to) !== identifier) return false;

  let ok = true;
  state.doc.nodesBetween(from, to, (node, _pos, parent) => {
    if (node.isText && isSkippedTextNode(node.marks, parent)) ok = false;
  });
  if (!ok) return false;

  const before = from > 0 ? state.doc.textBetween(from - 1, from) : '';
  const after = to < size ? state.doc.textBetween(to, to + 1) : '';
  if (before && BOUNDARY_RE.test(before)) return false;
  if (after && BOUNDARY_RE.test(after)) return false;
  return true;
}

export function createIssueIdentifierAutolinkExtension(
  options: IssueIdentifierAutolinkOptions,
): Extension {
  return Extension.create({
    name: 'issueIdentifierAutolink',

    addProseMirrorPlugins() {
      const maybeMentionType = this.editor.schema.nodes.mention;
      if (!maybeMentionType) return [];
      const mentionType: NodeType = maybeMentionType;
      const resolveRef = options.resolveRef;

      return [
        new Plugin<AutolinkPluginState>({
          key: pluginKey,
          state: {
            init: () => ({ pending: [], seq: 0 }),
            apply(tr, value, _oldState, newState): AutolinkPluginState {
              let pending = value.pending;
              let seq = value.seq;

              if (tr.docChanged && pending.length > 0) {
                pending = pending
                  .map((c) => ({
                    ...c,
                    from: tr.mapping.map(c.from, 1),
                    to: tr.mapping.map(c.to, -1),
                  }))
                  .filter((c) => c.from < c.to);
              }

              const remove = tr.getMeta(META_REMOVE) as number[] | undefined;
              if (remove && remove.length > 0) {
                const rm = new Set(remove);
                pending = pending.filter((c) => !rm.has(c.key));
              }

              const isUserEdit = tr.docChanged && !tr.getMeta('preventUpdate') && !remove;
              if (isUserEdit) {
                const fresh = candidatesFromUserTransaction(tr, newState);
                if (fresh.length > 0) {
                  pending = pending.concat(fresh.map((c) => ({ key: seq++, ...c })));
                }
              }

              if (pending === value.pending && seq === value.seq) return value;
              return { pending, seq };
            },
          },
          view(view) {
            const resultCache = new Map<string, ResolvedIssueRef | null>();
            const inFlight = new Set<string>();
            let destroyed = false;
            let scheduled = false;

            function scheduleApply(): void {
              if (scheduled || destroyed) return;
              scheduled = true;
              void Promise.resolve().then(() => {
                scheduled = false;
                if (!destroyed) applyReady(view);
              });
            }

            function applyReady(v: EditorView): void {
              const st = pluginKey.getState(v.state);
              if (!st || st.pending.length === 0) return;

              const removeKeys: number[] = [];
              const replacements: {
                from: number;
                to: number;
                ref: ResolvedIssueRef;
              }[] = [];

              for (const c of st.pending) {
                if (!resultCache.has(c.identifier)) continue;
                const ref = resultCache.get(c.identifier);
                removeKeys.push(c.key); 
                if (ref && rangeStillPlainIdentifier(v.state, c.from, c.to, c.identifier)) {
                  replacements.push({ from: c.from, to: c.to, ref });
                }
              }
              if (removeKeys.length === 0) return;

              const { tr } = v.state;
              replacements.sort((a, b) => b.from - a.from);
              for (const r of replacements) {
                tr.replaceWith(
                  r.from,
                  r.to,
                  mentionType.create({
                    id: r.ref.id,
                    label: r.ref.identifier,
                    type: 'issue',
                  }),
                );
              }
              tr.setMeta(META_REMOVE, removeKeys);
              v.dispatch(tr);
            }

            return {
              update() {
                if (destroyed) return;
                const resolve = resolveRef.current;
                if (!resolve) return;
                const st = pluginKey.getState(view.state);
                if (!st || st.pending.length === 0) return;

                let anyReady = false;
                for (const c of st.pending) {
                  if (resultCache.has(c.identifier)) {
                    anyReady = true;
                    continue;
                  }
                  if (inFlight.has(c.identifier)) continue;
                  inFlight.add(c.identifier);
                  Promise.resolve(resolve(c.identifier))
                    .then((ref) => {
                      resultCache.set(c.identifier, ref ?? null);
                      inFlight.delete(c.identifier);
                      scheduleApply();
                    })
                    .catch(() => {
                      resultCache.set(c.identifier, null);
                      inFlight.delete(c.identifier);
                      scheduleApply();
                    });
                }
                if (anyReady) scheduleApply();
              },
              destroy() {
                destroyed = true;
              },
            };
          },
        }),
      ];
    },
  });
}

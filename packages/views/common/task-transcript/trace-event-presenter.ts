// Презентер событий трассировки — чистый слой читаемости для ленты выполнения.

import type { TranscriptDetailDensity } from '@goosar/core/agents/stores';

export type { TranscriptDetailDensity };

export interface TraceEvent {
  seq?: number;
  type: string;
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
  created_at?: string;
}

export type TraceEventKind =
  'agent' | 'thinking' | 'tool_use' | 'tool_result' | 'error' | 'generic';

export function traceEventKind(event: TraceEvent): TraceEventKind {
  switch (event.type) {
    case 'text':
      return 'agent';
    case 'thinking':
      return 'thinking';
    case 'tool_use':
      return 'tool_use';
    case 'tool_result':
      return 'tool_result';
    case 'error':
      return 'error';
    default:
      return 'generic';
  }
}

export function traceEventLabel(event: TraceEvent): string {
  switch (event.type) {
    case 'text':
      return 'Agent';
    case 'thinking':
      return 'Thinking';
    case 'tool_use':
      return event.tool && event.tool.length > 0 ? event.tool : 'Tool';
    case 'tool_result':
      return event.tool && event.tool.length > 0 ? event.tool : 'Result';
    case 'error':
      return 'Error';
    default:
      return event.type && event.type.length > 0 ? event.type : 'Event';
  }
}

export function shortenTracePath(p: string): string {
  const parts = p.split('/');
  if (parts.length <= 3) return p;
  return '.../' + parts.slice(-2).join('/');
}

const SHELL_WRAPPER_PATTERN =
  /^(?:\/[\w./-]*\/)?(?:zsh|bash|sh|fish)\s+(?:-[a-z]+\s+)*(['"])([\s\S]+)\1$/;

export function stripShellWrapper(command: string): string {
  const match = SHELL_WRAPPER_PATTERN.exec(command.trim());
  return match?.[2] ?? command;
}

function clip(value: string, max: number): string {
  return value.length > max ? value.slice(0, max) + '...' : value;
}

export function traceToolArgSummary(input: Record<string, unknown> | undefined): string {
  if (!input) return '';
  const str = (v: unknown): string => (typeof v === 'string' ? v : '');
  if (str(input.query)) return str(input.query);
  if (str(input.file_path)) return shortenTracePath(str(input.file_path));
  if (str(input.path)) return shortenTracePath(str(input.path));
  if (str(input.pattern)) return str(input.pattern);
  if (str(input.description)) return str(input.description);
  if (str(input.command)) return clip(stripShellWrapper(str(input.command)), 120);
  if (str(input.prompt)) return clip(str(input.prompt), 120);
  if (str(input.skill)) return str(input.skill);
  for (const v of Object.values(input)) {
    if (typeof v === 'string' && v.length > 0 && v.length < 120) return v;
  }
  return '';
}

function firstLine(value: string | undefined): string {
  return value?.split('\n').find((l) => l.trim().length > 0) ?? '';
}

function collapseWhitespace(value: string | undefined): string {
  return (value ?? '').replace(/\s+/g, ' ').trim();
}

export function traceEventSummary(event: TraceEvent): string {
  switch (traceEventKind(event)) {
    case 'thinking':
      return clip(firstLine(event.content), 200);
    case 'tool_use':
      return traceToolArgSummary(event.input);
    case 'tool_result':
      return clip(collapseWhitespace(event.output), 200);
    default:
      return firstLine(event.content ?? event.output);
  }
}

export function traceEventCopyText(event: TraceEvent): string {
  const label = traceEventLabel(event);
  let body: string;
  switch (traceEventKind(event)) {
    case 'tool_use':
      body = event.input ? JSON.stringify(event.input, null, 2) : '';
      break;
    case 'tool_result':
      body = event.output ?? '';
      break;
    default:
      body = event.content ?? '';
  }
  const date = event.created_at ? new Date(event.created_at) : null;
  const timestamp = date && !Number.isNaN(date.getTime()) ? `[${date.toISOString()}] ` : '';
  return body ? `${timestamp}[${label}] ${body}` : `${timestamp}[${label}]`;
}

export function traceEventHasDetail(event: TraceEvent): boolean {
  switch (traceEventKind(event)) {
    case 'tool_use':
      return !!event.input && Object.keys(event.input).length > 0;
    case 'tool_result':
      return !!event.output && event.output.length > 0;
    default:
      return !!event.content && event.content.length > 0;
  }
}

export function traceEventSummaryIsMono(kind: TraceEventKind): boolean {
  return kind === 'tool_use' || kind === 'tool_result';
}

export function traceEventDefaultExpanded(
  event: TraceEvent,
  density: TranscriptDetailDensity,
): boolean {
  if (!traceEventHasDetail(event)) return false;
  switch (density) {
    case 'expanded':
      return true;
    case 'collapsed':
      return false;
    case 'smart': {
      const kind = traceEventKind(event);
      return kind === 'agent' || kind === 'error';
    }
  }
}

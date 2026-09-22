// Чистый парсер строк лога демона (Go slog).

export type LogLevel = 'DEBUG' | 'INFO' | 'WARN' | 'ERROR';

export interface ParsedLogLine {
  id: number;
  timestamp: string | null;
  level: LogLevel | null;
  message: string;
  fields: Record<string, string>;
  raw: string;
}

const HEADER_RE =
  /^(\d{2}:\d{2}:\d{2}\.\d{3})\s+(DEBUG|DBG|INFO|INF|WARN|WRN|ERROR|ERR)(?:[+-]\d+)?\s+(.+)$/;

const LEVEL_NORMALIZE: Record<string, LogLevel> = {
  DEBUG: 'DEBUG',
  DBG: 'DEBUG',
  INFO: 'INFO',
  INF: 'INFO',
  WARN: 'WARN',
  WRN: 'WARN',
  ERROR: 'ERROR',
  ERR: 'ERROR',
};
const TRAILING_FIELD_RE = /\s+([a-zA-Z_][a-zA-Z0-9_.]*)=("(?:[^"\\]|\\.)*"|\S+)$/;

function unquote(value: string): string {
  if (value.length >= 2 && value.startsWith('"') && value.endsWith('"')) {
    return value.slice(1, -1).replace(/\\"/g, '"').replace(/\\\\/g, '\\');
  }
  return value;
}

function extractTrailingFields(rest: string): {
  message: string;
  fields: Record<string, string>;
} {
  const fields: Record<string, string> = {};
  let work = rest;
  while (true) {
    const match = work.match(TRAILING_FIELD_RE);
    if (!match || match.index === undefined) break;
    fields[match[1]!] = unquote(match[2]!);
    work = work.slice(0, match.index);
  }
  return { message: work.trim(), fields };
}

export function parseLogLine(raw: string, id: number): ParsedLogLine {
  const match = raw.match(HEADER_RE);
  if (!match) {
    return { id, timestamp: null, level: null, message: raw, fields: {}, raw };
  }
  const [, timestamp, level, rest] = match;
  const normalized = LEVEL_NORMALIZE[level!];
  if (!normalized) {
    return { id, timestamp: null, level: null, message: raw, fields: {}, raw };
  }
  const { message, fields } = extractTrailingFields(rest!);
  return {
    id,
    timestamp: timestamp!,
    level: normalized,
    message,
    fields,
    raw,
  };
}

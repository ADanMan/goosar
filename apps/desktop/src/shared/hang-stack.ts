// Допустимая форма стека зависания и единственный код, который её строит.
// Кадры пересекают три границы: main → файл → preload → renderer.

export interface HangStackFrame {
  functionName: string;
  url: string;
  lineNumber: number;
  columnNumber: number;
}

export const HANG_STACK_MAX_FRAMES = 20;

export function sanitizeHangStackFrames(
  rawFrames: unknown,
  maxFrames: number = HANG_STACK_MAX_FRAMES,
): HangStackFrame[] | null {
  if (!Array.isArray(rawFrames) || rawFrames.length === 0) return null;

  const frames: HangStackFrame[] = [];
  for (const raw of rawFrames.slice(0, maxFrames)) {
    if (!raw || typeof raw !== 'object') continue;
    const frame = raw as Record<string, unknown>;
    const location = (frame.location ?? frame) as Record<string, unknown>;
    frames.push({
      functionName:
        typeof frame.functionName === 'string' && frame.functionName
          ? frame.functionName
          : '(anonymous)',
      url: redactFrameUrl(frame.url),
      lineNumber: toNumber(location.lineNumber),
      columnNumber: toNumber(location.columnNumber),
    });
  }
  return frames.length > 0 ? frames : null;
}

export function redactFrameUrl(url: unknown): string {
  if (typeof url !== 'string' || !url) return '';
  const [withoutQuery = ''] = url.split(/[?#]/);
  const segments = withoutQuery.split('/').filter(Boolean);
  if (segments.length === 0) return '';
  return segments.slice(-2).join('/');
}

function toNumber(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

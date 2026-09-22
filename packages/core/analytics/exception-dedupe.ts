// Дедупликация/троттлинг событий `$exception` в рамках сессии.

const STORAGE_KEY = 'mc_exc_fp';
const EXCEPTION_SAMPLE_LIMIT = 3;
const MAX_FINGERPRINTS = 50;

type FingerprintCounts = Record<string, number>;

export function shouldDropException(properties: Record<string, unknown> | undefined): boolean {
  const fingerprint = buildFingerprint(properties);
  if (fingerprint === null) return false;

  const storage = getSessionStorage();
  if (!storage) return false;

  try {
    const counts = readCounts(storage);
    const current = typeof counts[fingerprint] === 'number' ? counts[fingerprint] : 0;

    if (current >= EXCEPTION_SAMPLE_LIMIT) return true;

    if (current === 0 && Object.keys(counts).length >= MAX_FINGERPRINTS) {
      return false;
    }

    counts[fingerprint] = current + 1;
    try {
      storage.setItem(STORAGE_KEY, JSON.stringify(counts));
    } catch {
      // Persisting the increment failed (quota / disabled). We still keep this
      // event (return false below). The unpersisted increment only means the
      // next identical error is also kept — under-counting toward the limit,
      // i.e. fewer drops, never more. This is the required failure direction.
    }
    return false;
  } catch {
    return false;
  }
}

function readCounts(storage: Storage): FingerprintCounts {
  const raw = storage.getItem(STORAGE_KEY);
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as FingerprintCounts;
    }
  } catch {
    // Corrupt JSON blob → start fresh.
  }
  return {};
}

function buildFingerprint(properties: Record<string, unknown> | undefined): string | null {
  if (!properties || typeof properties !== 'object') return null;

  const list = properties.$exception_list;
  const entry =
    Array.isArray(list) && list.length > 0 && list[0] && typeof list[0] === 'object'
      ? (list[0] as Record<string, unknown>)
      : undefined;

  const type = readString(entry?.type) ?? readString(properties.$exception_type) ?? '';
  const value = readString(entry?.value) ?? readString(properties.$exception_message) ?? '';
  const frame = topFrame(entry);

  if (type === '' && value === '' && !frame) return null;

  const parts = [type, value];
  if (frame) {
    parts.push(frame.filename, frame.fn, frame.lineno, frame.colno);
  }
  return hash(parts.join(''));
}

interface TopFrame {
  filename: string;
  fn: string;
  lineno: string;
  colno: string;
}

function topFrame(entry: Record<string, unknown> | undefined): TopFrame | null {
  if (!entry) return null;
  const stacktrace = entry.stacktrace;
  const frames =
    stacktrace && typeof stacktrace === 'object'
      ? (stacktrace as Record<string, unknown>).frames
      : undefined;
  if (!Array.isArray(frames) || frames.length === 0) return null;

  const f = frames[frames.length - 1];
  if (!f || typeof f !== 'object') return null;
  const frame = f as Record<string, unknown>;

  return {
    filename: readString(frame.filename) ?? '',
    fn: readString(frame.function) ?? '',
    lineno: readNumberAsString(frame.lineno) ?? '',
    colno: readNumberAsString(frame.colno) ?? '',
  };
}

function readString(v: unknown): string | undefined {
  return typeof v === 'string' && v.length > 0 ? v : undefined;
}

function readNumberAsString(v: unknown): string | undefined {
  return typeof v === 'number' && Number.isFinite(v) ? String(v) : undefined;
}

function hash(input: string): string {
  let h = 5381;
  for (let i = 0; i < input.length; i++) {
    h = ((h << 5) + h) ^ input.charCodeAt(i);
  }
  return (h >>> 0).toString(36);
}

function getSessionStorage(): Storage | null {
  try {
    if (typeof sessionStorage === 'undefined') return null;
    return sessionStorage;
  } catch {
    return null;
  }
}

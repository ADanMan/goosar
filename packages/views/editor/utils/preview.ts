// Таблица диспетчеризации превью для AttachmentPreviewModal; новые типы
// добавляются здесь.

export type PreviewKind = 'image' | 'pdf' | 'video' | 'audio' | 'markdown' | 'html' | 'text';

const EXT_LANGUAGE_MAP: Record<string, string> = {
  md: 'markdown',
  markdown: 'markdown',
  txt: 'plaintext',
  log: 'plaintext',
  html: 'xml',
  htm: 'xml',
  xml: 'xml',
  svg: 'xml',
  css: 'css',
  scss: 'scss',
  sass: 'scss',
  less: 'less',
  json: 'json',
  yml: 'yaml',
  yaml: 'yaml',
  toml: 'ini',
  ini: 'ini',
  conf: 'ini',
  dockerfile: 'dockerfile',
  makefile: 'makefile',
  gitignore: 'plaintext',
  sh: 'bash',
  bash: 'bash',
  zsh: 'bash',
  py: 'python',
  rb: 'ruby',
  go: 'go',
  rs: 'rust',
  ts: 'typescript',
  tsx: 'typescript',
  js: 'javascript',
  jsx: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  java: 'java',
  kt: 'kotlin',
  swift: 'swift',
  c: 'c',
  cc: 'cpp',
  cpp: 'cpp',
  h: 'c',
  hpp: 'cpp',
  cs: 'csharp',
  php: 'php',
  lua: 'lua',
  vim: 'vim',
  sql: 'sql',
  csv: 'plaintext',
  tsv: 'plaintext',
};

const BASENAME_LANGUAGE_MAP: Record<string, string> = {
  dockerfile: 'dockerfile',
  makefile: 'makefile',
  '.env': 'plaintext',
  '.gitignore': 'plaintext',
};

const TEXT_EXTENSIONS = new Set<string>([
  'md',
  'markdown',
  'txt',
  'log',
  'csv',
  'tsv',
  'html',
  'htm',
  'json',
  'xml',
  'yml',
  'yaml',
  'toml',
  'ini',
  'conf',
  'dockerfile',
  'makefile',
  'gitignore',
  'sh',
  'bash',
  'zsh',
  'py',
  'rb',
  'go',
  'rs',
  'ts',
  'tsx',
  'js',
  'jsx',
  'mjs',
  'cjs',
  'css',
  'scss',
  'sass',
  'less',
  'sql',
  'java',
  'kt',
  'swift',
  'c',
  'cc',
  'cpp',
  'h',
  'hpp',
  'cs',
  'php',
  'lua',
  'vim',
]);

const TEXT_CONTENT_TYPES = new Set<string>([
  'application/json',
  'application/javascript',
  'application/xml',
  'application/x-yaml',
  'application/yaml',
  'application/toml',
  'application/x-sh',
  'application/x-httpd-php',
]);

const TEXT_BASENAMES = new Set<string>(['dockerfile', 'makefile', '.env', '.gitignore']);

const VIDEO_EXTS = new Set<string>(['mp4', 'm4v', 'mov', 'webm', 'mkv', 'avi', 'ogv']);
const AUDIO_EXTS = new Set<string>(['mp3', 'wav', 'm4a', 'ogg', 'oga', 'flac', 'aac', 'opus']);
const IMAGE_EXTS = new Set<string>([
  'png',
  'jpg',
  'jpeg',
  'gif',
  'webp',
  'avif',
  'bmp',
  'ico',
  'svg',
]);

function extOf(filename: string): string {
  const base = filename.toLowerCase().split(/[\\/]/).pop() ?? '';
  const dot = base.lastIndexOf('.');
  if (dot <= 0) return '';
  return base.slice(dot + 1);
}

function baseOf(filename: string): string {
  return (filename.toLowerCase().split(/[\\/]/).pop() ?? '').trim();
}

function normalizeContentType(contentType: string): string {
  const ct = (contentType ?? '').toLowerCase().trim();
  const semi = ct.indexOf(';');
  return (semi >= 0 ? ct.slice(0, semi) : ct).trim();
}

function isTextLike(contentType: string, filename: string): boolean {
  const ct = normalizeContentType(contentType);
  if (ct.startsWith('text/')) return true;
  if (TEXT_CONTENT_TYPES.has(ct)) return true;
  const ext = extOf(filename);
  if (ext && TEXT_EXTENSIONS.has(ext)) return true;
  return TEXT_BASENAMES.has(baseOf(filename));
}

export function getPreviewKind(contentType: string, filename: string): PreviewKind | null {
  const ct = normalizeContentType(contentType);

  const ext = extOf(filename);

  if (ct === 'application/pdf' || ext === 'pdf') return 'pdf';
  if (ct.startsWith('video/') || (ext && VIDEO_EXTS.has(ext))) return 'video';
  if (ct.startsWith('audio/') || (ext && AUDIO_EXTS.has(ext))) return 'audio';

  if (ct.startsWith('image/') || (ext && IMAGE_EXTS.has(ext))) return 'image';

  if (ct === 'text/markdown' || ext === 'md' || ext === 'markdown') {
    return 'markdown';
  }
  if (ct === 'text/html' || ext === 'html' || ext === 'htm') {
    return 'html';
  }

  if (isTextLike(contentType, filename)) return 'text';
  return null;
}

export function isPreviewable(contentType: string, filename: string): boolean {
  return getPreviewKind(contentType, filename) !== null;
}

export function extensionToLanguage(filename: string): string | undefined {
  const ext = extOf(filename);
  if (ext && EXT_LANGUAGE_MAP[ext]) return EXT_LANGUAGE_MAP[ext];
  const base = baseOf(filename);
  if (BASENAME_LANGUAGE_MAP[base]) return BASENAME_LANGUAGE_MAP[base];
  return undefined;
}

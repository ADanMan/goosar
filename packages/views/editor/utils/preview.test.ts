import { describe, expect, it } from 'vitest';
import { extensionToLanguage, getPreviewKind, isPreviewable, type PreviewKind } from './preview';

describe('getPreviewKind', () => {
  const cases: Array<[string, string, PreviewKind | null]> = [
    ['application/pdf', 'manual.pdf', 'pdf'],
    ['video/mp4', 'clip.mp4', 'video'],
    ['audio/mpeg', 'note.mp3', 'audio'],

    ['text/markdown', 'README', 'markdown'],
    ['text/plain', 'README.md', 'markdown'],
    ['application/octet-stream', 'notes.markdown', 'markdown'],

    ['text/html', 'page', 'html'],
    ['application/octet-stream', 'page.html', 'html'],

    ['text/plain', 'main.go', 'text'],
    ['application/octet-stream', 'main.go', 'text'],
    ['text/plain', 'config.yml', 'text'],
    ['application/javascript', 'bundle.js', 'text'],
    ['application/json', 'data.json', 'text'],

    ['text/plain', 'log.txt', 'text'],

    ['application/octet-stream', 'Dockerfile', 'text'],
    ['application/octet-stream', 'Makefile', 'text'],
    ['application/octet-stream', '.env', 'text'],
    ['application/octet-stream', '.gitignore', 'text'],
    ['application/octet-stream', 'service.dockerfile', 'text'],
    ['application/octet-stream', 'rules.makefile', 'text'],

    [
      'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      'report.docx',
      null,
    ],
    ['application/octet-stream', 'blob.bin', null],
    ['application/zip', 'archive.zip', null],
  ];

  for (const [ct, filename, want] of cases) {
    it(`(${ct}, ${filename}) → ${want}`, () => {
      expect(getPreviewKind(ct, filename)).toBe(want);
    });
  }

  it('falls through to extension when content_type is mislabeled', () => {
    expect(getPreviewKind('application/octet-stream', 'manual.pdf')).toBe('pdf');
  });
});

describe('isPreviewable', () => {
  it('is true for any non-null PreviewKind', () => {
    expect(isPreviewable('application/pdf', 'x.pdf')).toBe(true);
    expect(isPreviewable('text/plain', 'x.txt')).toBe(true);
  });

  it('is false for unsupported types', () => {
    expect(isPreviewable('application/zip', 'x.zip')).toBe(false);
    expect(isPreviewable('application/octet-stream', 'x.bin')).toBe(false);
  });
});

describe('extensionToLanguage', () => {
  it('maps common code extensions to hljs language tokens', () => {
    expect(extensionToLanguage('index.ts')).toBe('typescript');
    expect(extensionToLanguage('main.go')).toBe('go');
    expect(extensionToLanguage('script.py')).toBe('python');
    expect(extensionToLanguage('style.scss')).toBe('scss');
  });

  it('falls back to plaintext for non-code text files', () => {
    expect(extensionToLanguage('log.txt')).toBe('plaintext');
  });

  it('recognizes extension-less build files', () => {
    expect(extensionToLanguage('Dockerfile')).toBe('dockerfile');
    expect(extensionToLanguage('Makefile')).toBe('makefile');
    expect(extensionToLanguage('.env')).toBe('plaintext');
    expect(extensionToLanguage('.gitignore')).toBe('plaintext');
  });

  it('recognizes build file extensions allowed by the server', () => {
    expect(extensionToLanguage('service.dockerfile')).toBe('dockerfile');
    expect(extensionToLanguage('rules.makefile')).toBe('makefile');
    expect(extensionToLanguage('nested/.gitignore')).toBe('plaintext');
  });

  it('returns undefined for unknown extensions', () => {
    expect(extensionToLanguage('blob.bin')).toBeUndefined();
    expect(extensionToLanguage('noextension')).toBeUndefined();
  });
});

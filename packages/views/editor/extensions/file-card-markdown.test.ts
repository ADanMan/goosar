import { describe, it, expect } from 'vitest';
import { FileCardExtension } from './file-card';
import { ImageExtension } from './index';
import { preprocessFileCards } from '@goosar/ui/markdown';

const fileCardRenderMarkdown = FileCardExtension.config.renderMarkdown as (node: {
  attrs: Record<string, string>;
}) => string;

const tokenizer = FileCardExtension.config.markdownTokenizer!;
const tokenize = tokenizer.tokenize as (
  src: string,
) => { type: string; raw: string; attributes: Record<string, string> } | undefined;

const imageRenderMarkdown = ImageExtension.config.renderMarkdown as (node: {
  attrs: Record<string, string>;
}) => string;

describe('ImageExtension.renderMarkdown', () => {
  it('escapes special chars in alt text', () => {
    const md = imageRenderMarkdown({
      attrs: { src: 'https://cdn.example.com/img.png', alt: '6P4N\\`X[A~Z(S@XO}WE0FT_P.jpg' },
    });
    expect(md).toContain('\\\\');
    expect(md).toContain('\\[');
    expect(md).toContain('\\(');
    expect(md).toMatch(/^!\[.*\]\(https:\/\/cdn\.example\.com\/img\.png\)$/);
  });

  it('leaves normal alt text unchanged', () => {
    const md = imageRenderMarkdown({
      attrs: { src: 'https://cdn.example.com/img.png', alt: 'screenshot' },
    });
    expect(md).toBe('![screenshot](https://cdn.example.com/img.png)');
  });
});

describe('in-flight placeholders never serialise', () => {
  it('emits nothing for an uploading fileCard', () => {
    expect(
      fileCardRenderMarkdown({
        attrs: { filename: 'x.pdf', href: '', uploading: true } as never,
      }),
    ).toBe('');
  });

  it('emits nothing for a fileCard with no href, even when not marked uploading', () => {
    expect(fileCardRenderMarkdown({ attrs: { filename: 'x.pdf', href: '' } as never })).toBe('');
  });

  it('emits nothing for an uploading image', () => {
    expect(
      imageRenderMarkdown({
        attrs: { src: 'blob:http://localhost/abc', alt: 'x.png', uploading: true } as never,
      }),
    ).toBe('');
  });

  it('emits normally once the upload settled into a real URL', () => {
    expect(
      fileCardRenderMarkdown({
        attrs: {
          filename: 'x.pdf',
          href: '/api/attachments/a1/download',
          uploading: false,
        } as never,
      }),
    ).toBe('!file[x.pdf](/api/attachments/a1/download)');
    expect(
      imageRenderMarkdown({
        attrs: { src: '/api/attachments/a2/download', alt: 'x.png', uploading: false } as never,
      }),
    ).toBe('![x.png](/api/attachments/a2/download)');
  });
});

describe('file-card tokenizer', () => {
  it('round-trips a filename with all special chars', () => {
    const filename = 'report[final](v2)\\draft.pdf';
    const md = fileCardRenderMarkdown({
      attrs: { href: 'https://cdn.example.com/f.pdf', filename },
    });
    const token = tokenize(md);
    expect(token).toBeDefined();
    expect(token!.attributes.filename).toBe(filename);
    expect(token!.attributes.href).toBe('https://cdn.example.com/f.pdf');
  });

  it('round-trips a normal filename', () => {
    const md = fileCardRenderMarkdown({
      attrs: { href: 'https://cdn.example.com/readme.md', filename: 'readme.md' },
    });
    const token = tokenize(md);
    expect(token).toBeDefined();
    expect(token!.attributes.filename).toBe('readme.md');
  });

  it('rejects an unterminated file card with escape-pair runs in linear time', () => {
    const src = `!file[${'\\a'.repeat(28)}](/uploads/x`;

    const t0 = performance.now();
    const token = tokenize(src);
    const elapsed = performance.now() - t0;

    expect(token).toBeUndefined();
    expect(elapsed).toBeLessThan(100);
  });
});

describe('preprocessFileCards', () => {
  it('converts escaped file-card syntax and unescapes the filename', () => {
    const input = '!file[notes\\[v2\\]\\(draft\\).txt](https://cdn.example.com/notes.txt)';
    const result = preprocessFileCards(input, 'cdn.example.com');
    expect(result).toContain('data-type="fileCard"');
    expect(result).toContain('data-filename="notes[v2](draft).txt"');
    expect(result).toContain('data-href="https://cdn.example.com/notes.txt"');
  });

  it('converts a normal file-card syntax', () => {
    const input = '!file[readme.md](https://cdn.example.com/readme.md)';
    const result = preprocessFileCards(input, 'cdn.example.com');
    expect(result).toContain('data-type="fileCard"');
    expect(result).toContain('data-filename="readme.md"');
  });

  it('rejects an unterminated file card with escape-pair runs in linear time', () => {
    const input = `!file[${'\\a'.repeat(28)}](/uploads/x`;

    const t0 = performance.now();
    const result = preprocessFileCards(input, 'cdn.example.com');
    const elapsed = performance.now() - t0;

    expect(result).toBe(input);
    expect(elapsed).toBeLessThan(100);
  });
});

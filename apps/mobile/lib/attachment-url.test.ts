/**
 * Тесты резолвера URL вложений для мобильного клиента. Проверяется вариант
 * с явно переданным базовым URL, так как основная функция привязывается
 * к process.env при загрузке модуля, а это нежелательно мутировать в тестах.
 */
import { describe, expect, it } from 'vitest';
import { resolveAttachmentUrl, resolveAttachmentUrlWithBase } from './attachment-url';

describe('resolveAttachmentUrlWithBase', () => {
  const BASE = 'https://api.example.test';

  it('prepends the API base for a server-relative path', () => {
    expect(resolveAttachmentUrlWithBase('/api/attachments/att-1/download', BASE)).toBe(
      'https://api.example.test/api/attachments/att-1/download',
    );
  });

  it('trims a trailing slash on the API base before joining', () => {
    expect(
      resolveAttachmentUrlWithBase('/api/attachments/att-1/download', 'https://api.example.test/'),
    ).toBe('https://api.example.test/api/attachments/att-1/download');
  });

  it('passes an absolute https URL through unchanged (CloudFront / presigned)', () => {
    const signed = 'https://cdn.example.test/att-1.bin?Policy=p&Signature=s&Key-Pair-Id=k';
    expect(resolveAttachmentUrlWithBase(signed, BASE)).toBe(signed);
  });

  it('passes an absolute http URL through unchanged (self-hosted dev)', () => {
    expect(resolveAttachmentUrlWithBase('http://localhost:8080/file.bin', BASE)).toBe(
      'http://localhost:8080/file.bin',
    );
  });

  it('returns null for nullish or empty input', () => {
    expect(resolveAttachmentUrlWithBase(null, BASE)).toBeNull();
    expect(resolveAttachmentUrlWithBase(undefined, BASE)).toBeNull();
    expect(resolveAttachmentUrlWithBase('', BASE)).toBeNull();
  });

  it('keeps a relative path unchanged when the base is empty (web same-origin convention)', () => {
    expect(resolveAttachmentUrlWithBase('/api/attachments/att-1/download', '')).toBe(
      '/api/attachments/att-1/download',
    );
  });
});

describe('composer file chip — completed non-image attachment', () => {
  const BASE = 'https://api.example.test';
  const completedFileChip = {
    localId: 'local-1',
    localUri: 'file:///private/var/.../IMG_0001.pdf',
    filename: 'report.pdf',
    mimeType: 'application/pdf',
    status: 'completed' as const,
    id: 'att-42',
    url: 'mc://file/att-42',
    downloadUrl: '/api/attachments/att-42/download',
  };

  it('resolves a server-relative downloadUrl against the API base', () => {
    expect(resolveAttachmentUrlWithBase(completedFileChip.downloadUrl, BASE)).toBe(
      'https://api.example.test/api/attachments/att-42/download',
    );
  });

  it('preserves an absolute downloadUrl returned by CloudFront / presign', () => {
    const cloudFront = {
      ...completedFileChip,
      downloadUrl: 'https://cdn.example.test/att-42.pdf?Signature=s&Key-Pair-Id=k',
    };
    expect(resolveAttachmentUrlWithBase(cloudFront.downloadUrl, BASE)).toBe(cloudFront.downloadUrl);
  });

  it("returns null when the upload hasn't populated downloadUrl yet (no Linking call)", () => {
    const partial = { ...completedFileChip, downloadUrl: undefined };
    expect(resolveAttachmentUrlWithBase(partial.downloadUrl, BASE)).toBeNull();
  });
});

describe('resolveAttachmentUrl (env-bound)', () => {
  it('matches the with-base form for an absolute URL regardless of EXPO_PUBLIC_API_URL', () => {
    const absolute = 'https://cdn.example.test/file.pdf?Signature=s';
    expect(resolveAttachmentUrl(absolute)).toBe(absolute);
  });

  it('returns null for empty input', () => {
    expect(resolveAttachmentUrl(undefined)).toBeNull();
    expect(resolveAttachmentUrl(null)).toBeNull();
    expect(resolveAttachmentUrl('')).toBeNull();
  });
});

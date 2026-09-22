/**
 * Тесты классификации политики внешних изображений в markdown. Проверяются
 * чистые функции напрямую, так как vitest-окружение мобильного клиента без
 * RN-рендерера не может отрендерить сам компонент изображения.
 */
import { describe, expect, it } from 'vitest';
import {
  buildTrustedImageHosts,
  isTrustedMarkdownImageUri,
  normalizeExternalImagesMode,
} from './image-policy';

const NO_HOSTS: ReadonlySet<string> = new Set();

describe('normalizeExternalImagesMode', () => {
  it('defaults absent/empty/allow to allow (old servers, failed config fetch)', () => {
    expect(normalizeExternalImagesMode(undefined)).toBe('allow');
    expect(normalizeExternalImagesMode('')).toBe('allow');
    expect(normalizeExternalImagesMode('allow')).toBe('allow');
    expect(normalizeExternalImagesMode(' Allow ')).toBe('allow');
  });

  it('recognizes allowlist and fails closed on unknown values', () => {
    expect(normalizeExternalImagesMode('allowlist')).toBe('allowlist');
    expect(normalizeExternalImagesMode('block')).toBe('block');
    expect(normalizeExternalImagesMode('blok')).toBe('block');
  });
});

describe('buildTrustedImageHosts', () => {
  it('normalizes bare hosts, URLs, and drops empties', () => {
    const hosts = buildTrustedImageHosts({
      imageHosts: ['CDN.Example.com', 'https://media.example.com:8443/base', ''],
      cdnDomain: 'goosar-static.example.com',
      apiBaseUrl: 'https://api.example.com',
    });
    expect(hosts).toEqual(
      new Set([
        'cdn.example.com',
        'media.example.com',
        'goosar-static.example.com',
        'api.example.com',
      ]),
    );
  });
});

describe('isTrustedMarkdownImageUri', () => {
  it('allow mode keeps external http(s) images (previous behavior)', () => {
    expect(isTrustedMarkdownImageUri('https://anything.example/x.png', 'allow', NO_HOSTS)).toBe(
      true,
    );
  });

  it('block mode rejects external http(s) images', () => {
    expect(isTrustedMarkdownImageUri('https://attacker.example/x.png', 'block', NO_HOSTS)).toBe(
      false,
    );
  });

  it('always trusts site-relative attachment refs and data:image URIs', () => {
    for (const mode of ['allow', 'block', 'allowlist'] as const) {
      expect(
        isTrustedMarkdownImageUri(
          '/api/attachments/0195c9a1-1111-7bbb-8ccc-abcdefabcdef/download',
          mode,
          NO_HOSTS,
        ),
      ).toBe(true);
      expect(isTrustedMarkdownImageUri('/uploads/ws1/pic.png', mode, NO_HOSTS)).toBe(true);
      expect(isTrustedMarkdownImageUri('data:image/png;base64,iVBORw0KGgo=', mode, NO_HOSTS)).toBe(
        true,
      );
    }
  });

  it('allowlist mode admits trusted hosts only, case-insensitive, port ignored', () => {
    const hosts = buildTrustedImageHosts({ imageHosts: ['cdn.example.com'] });
    expect(
      isTrustedMarkdownImageUri('https://CDN.example.com:8443/x.png', 'allowlist', hosts),
    ).toBe(true);
    expect(isTrustedMarkdownImageUri('https://sub.cdn.example.com/x.png', 'allowlist', hosts)).toBe(
      false,
    );
  });

  it('legacy absolute att.url on the deployment CDN stays renderable in block mode', () => {
    const hosts = buildTrustedImageHosts({ cdnDomain: 'goosar-static.example.com' });
    expect(
      isTrustedMarkdownImageUri(
        'https://goosar-static.example.com/uploads/ws1/old.png',
        'block',
        hosts,
      ),
    ).toBe(true);
  });

  it('blocks protocol-relative, backslash-disguised, and non-image data URIs in block mode', () => {
    expect(isTrustedMarkdownImageUri('//attacker.example/x.png', 'block', NO_HOSTS)).toBe(false);
    expect(isTrustedMarkdownImageUri('/\\attacker.example/x.png', 'block', NO_HOSTS)).toBe(false);
    expect(isTrustedMarkdownImageUri('data:text/html,<script/>', 'block', NO_HOSTS)).toBe(false);
  });

  it('rejects non-http(s) schemes (unresolved mc://) in restrictive modes', () => {
    expect(isTrustedMarkdownImageUri('mc://file/abc', 'block', NO_HOSTS)).toBe(false);
  });
});

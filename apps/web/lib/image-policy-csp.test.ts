// Тесты img-src CSP документов по режиму внешних изображений; семантика должна
// совпадать с Go-middleware.
import { describe, expect, it } from 'vitest';
import { buildDocumentImageCSP, externalImagesMode } from './image-policy-csp';

describe('externalImagesMode', () => {
  it('defaults absent/empty/allow to allow', () => {
    expect(externalImagesMode({})).toBe('allow');
    expect(externalImagesMode({ GOOSAR_EXTERNAL_IMAGES: '' })).toBe('allow');
    expect(externalImagesMode({ GOOSAR_EXTERNAL_IMAGES: ' Allow ' })).toBe('allow');
  });

  it('recognizes allowlist and fails closed on unknown values', () => {
    expect(externalImagesMode({ GOOSAR_EXTERNAL_IMAGES: 'allowlist' })).toBe('allowlist');
    expect(externalImagesMode({ GOOSAR_EXTERNAL_IMAGES: 'block' })).toBe('block');
    expect(externalImagesMode({ GOOSAR_EXTERNAL_IMAGES: 'blok' })).toBe('block');
  });
});

describe('buildDocumentImageCSP', () => {
  it('allow mode emits NO header — previous behavior, nothing can break', () => {
    expect(buildDocumentImageCSP({})).toBeNull();
    expect(buildDocumentImageCSP({ GOOSAR_EXTERNAL_IMAGES: 'allow' })).toBeNull();
  });

  it("block mode: 'self' data: plus own storage hosts, operator list ignored", () => {
    const csp = buildDocumentImageCSP({
      GOOSAR_EXTERNAL_IMAGES: 'block',
      GOOSAR_IMAGE_HOSTS: 'cdn.example.com',
      CLOUDFRONT_DOMAIN: 'media.goosar.example',
      LOCAL_UPLOAD_BASE_URL: 'http://localhost:8081',
    });
    expect(csp).toBe("img-src 'self' data: media.goosar.example http://localhost:8081");
  });

  it('allowlist mode appends sanitized operator hosts', () => {
    const csp = buildDocumentImageCSP({
      GOOSAR_EXTERNAL_IMAGES: 'allowlist',
      GOOSAR_IMAGE_HOSTS: ' cdn.example.com , https://media.example.com:8443 ',
    });
    expect(csp).toBe("img-src 'self' data: cdn.example.com https://media.example.com:8443");
  });

  it('drops header-injection and wildcard entries, dedupes storage hosts', () => {
    const csp = buildDocumentImageCSP({
      GOOSAR_EXTERNAL_IMAGES: 'allowlist',
      GOOSAR_IMAGE_HOSTS:
        'evil.com; script-src *,ok.example.com,*.wild.example.com,javascript://x,cdn.same.example',
      CLOUDFRONT_DOMAIN: 'cdn.same.example',
    });
    expect(csp).toBe("img-src 'self' data: cdn.same.example ok.example.com");
  });

  it('invalid mode fails closed to block', () => {
    expect(buildDocumentImageCSP({ GOOSAR_EXTERNAL_IMAGES: 'blok' })).toBe("img-src 'self' data:");
  });
});

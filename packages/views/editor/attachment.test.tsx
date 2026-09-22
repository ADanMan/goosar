import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { Attachment as AttachmentRecord } from '@goosar/core/types';

const {
  getAttachmentTextContentMock,
  getAttachmentMock,
  getBaseUrlMock,
  downloadMock,
  openExternalMock,
  openByUrlMock,
} = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
  getAttachmentMock: vi.fn(),
  getBaseUrlMock: vi.fn(() => ''),
  downloadMock: vi.fn(),
  openExternalMock: vi.fn(),
  openByUrlMock: vi.fn(),
}));

vi.mock('@goosar/core/api', () => ({
  api: {
    getAttachmentTextContent: getAttachmentTextContentMock,
    getAttachment: getAttachmentMock,
    getBaseUrl: getBaseUrlMock,
  },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock('./use-download-attachment', () => ({
  useDownloadAttachment: () => downloadMock,
}));

vi.mock('../platform', () => ({
  openExternal: openExternalMock,
}));

vi.mock('../i18n', () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        image: {
          view: 'View',
          download: 'Download',
          copy_link: 'Copy link',
          copy_link_failed: 'Copy failed',
          link_copied: 'Link copied',
          delete: 'Delete',
        },
        attachment: {
          preview: 'Preview',
          preview_loading: 'Loading preview…',
          preview_failed: "Couldn't load preview",
          preview_unsupported: "This file type can't be previewed.",
          preview_too_large: 'File is too large to preview.',
          open_in_new_tab: 'Open in new tab',
          close: 'Close',
        },
        file_card: { uploading: 'Uploading {{filename}}' },
      }),
  }),
}));

vi.mock('../navigation', () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: '/acme/issues',
    searchParams: new URLSearchParams(),
    openInNewTab: vi.fn(),
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock('@goosar/core/paths', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/paths')>();
  return {
    ...actual,
    useWorkspaceSlug: () => 'acme',
    useWorkspacePaths: () => actual.paths.workspace('acme'),
  };
});

const resolverState: { attachments: AttachmentRecord[] } = { attachments: [] };
function attachmentIdFromTestDownloadURL(url: string): string | undefined {
  const path = /^https?:\/\//i.test(url)
    ? (() => {
        try {
          return new URL(url).pathname;
        } catch {
          return '';
        }
      })()
    : (url.split(/[?#]/, 1)[0] ?? '');
  const match = path.match(/^\/api\/attachments\/([^/]+)\/download$/);
  return match?.[1];
}
vi.mock('./attachment-download-context', () => ({
  useAttachmentDownloadResolver: () => ({
    resolveAttachmentId: (url: string) =>
      resolverState.attachments.find((a) => {
        const id = attachmentIdFromTestDownloadURL(url);
        return a.url === url || (id !== undefined && a.id === id);
      })?.id,
    resolveAttachment: (url: string) =>
      resolverState.attachments.find((a) => {
        const id = attachmentIdFromTestDownloadURL(url);
        return a.url === url || (id !== undefined && a.id === id);
      }),
    openByUrl: openByUrlMock,
  }),
  AttachmentDownloadProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

import { Attachment } from './attachment';
import { configStore } from '@goosar/core/config';

function makeRecord(overrides: Partial<AttachmentRecord> = {}): AttachmentRecord {
  return {
    id: 'att-1',
    workspace_id: 'ws-1',
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: 'member',
    uploader_id: 'u-1',
    filename: 'shot.png',
    url: 'https://cdn.example.test/att-1.png',
    download_url: 'https://cdn.example.test/att-1.png?Signature=s',
    markdown_url: 'https://cdn.example.test/api/attachments/att-1/download',
    content_type: 'image/png',
    size_bytes: 1024,
    created_at: '2026-05-13T00:00:00Z',
    ...overrides,
  };
}

function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  resolverState.attachments = [];
  configStore.setState({ cdnDomain: '', cdnSigned: false });
  getBaseUrlMock.mockReturnValue('');
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('Attachment — image dispatch', () => {
  it('record image renders <img> with hover toolbar (View/Download/Copy)', () => {
    const att = makeRecord();
    renderWithQuery(<Attachment attachment={{ kind: 'record', attachment: att }} />);
    const img = document.querySelector('img');
    expect(img).toBeTruthy();
    expect(img?.getAttribute('src')).toBe(att.download_url);
    expect(img?.getAttribute('alt')).toBe('shot.png');
    expect(screen.getByTitle('View')).toBeTruthy();
    expect(screen.getByTitle('Download')).toBeTruthy();
    expect(screen.getByTitle('Copy link')).toBeTruthy();
    expect(screen.queryByTitle('Delete')).toBeNull();
  });

  it('editable image shows Trash button and wires onDelete', () => {
    const att = makeRecord();
    const onDelete = vi.fn();
    renderWithQuery(
      <Attachment attachment={{ kind: 'record', attachment: att }} editable onDelete={onDelete} />,
    );
    const trash = screen.getByTitle('Delete');
    fireEvent.click(trash);
    expect(onDelete).toHaveBeenCalled();
  });

  it('url-only image resolves to a record via context and uses its id for download', () => {
    const att = makeRecord({
      filename: 'from-resolver.png',
      url: 'https://cdn.example.test/from-resolver.png',
    });
    resolverState.attachments = [att];
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: att.url,
          filename: 'from-resolver.png',
        }}
      />,
    );
    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe(att.download_url);
    fireEvent.click(screen.getByTitle('Download'));
    expect(downloadMock).toHaveBeenCalledWith('att-1');
  });

  it('renders the configured CDN URL when description markdown stores the stable API URL', () => {
    configStore.setState({ cdnDomain: 'cdn.example.test' });
    const id = '11111111-2222-3333-4444-555555555555';
    const markdownUrl = `https://api.goosar.ru/api/attachments/${id}/download`;
    const att = makeRecord({
      id,
      url: 'https://cdn.example.test/uploads/ws/shot.png',
      markdown_url: markdownUrl,
      download_url: `/api/attachments/${id}/download`,
    });
    resolverState.attachments = [att];

    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: markdownUrl,
          filename: 'shot.png',
          forceKind: 'image',
        }}
      />,
    );

    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe('https://cdn.example.test/uploads/ws/shot.png');
  });

  it('prefers a local disk /uploads URL over API markdown in split-origin self-host', () => {
    getBaseUrlMock.mockReturnValue('https://api.example.test');
    const id = '11111111-2222-3333-4444-555555555555';
    const markdownUrl = `https://api.example.test/api/attachments/${id}/download`;
    const mediaUrl = 'https://api.example.test/uploads/workspaces/ws-1/shot.png';
    const att = makeRecord({
      id,
      url: '/uploads/workspaces/ws-1/shot.png',
      markdown_url: markdownUrl,
      download_url: `/api/attachments/${id}/download`,
    });
    resolverState.attachments = [att];

    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: markdownUrl,
          filename: 'shot.png',
          forceKind: 'image',
        }}
      />,
    );

    expect(document.querySelector('img')?.getAttribute('src')).toBe(mediaUrl);

    fireEvent.click(screen.getByTitle('View'));

    const imageSrcs = [...document.querySelectorAll('img')].map((img) => img.getAttribute('src'));
    expect(imageSrcs).toEqual([mediaUrl, mediaUrl]);
  });

  it('opens preview with the same resolved media URL when a reopened draft record has no download_url', () => {
    configStore.setState({ cdnDomain: 'cdn.example.test' });
    const id = '11111111-2222-3333-4444-555555555555';
    const markdownUrl = `https://api.goosar.ru/api/attachments/${id}/download`;
    const mediaUrl = 'https://cdn.example.test/uploads/ws/shot.png';
    const att = makeRecord({
      id,
      url: mediaUrl,
      markdown_url: markdownUrl,
      download_url: '',
    });
    resolverState.attachments = [att];

    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: markdownUrl,
          filename: 'shot.png',
          forceKind: 'image',
        }}
      />,
    );

    fireEvent.click(screen.getByTitle('View'));

    const imageSrcs = [...document.querySelectorAll('img')].map((img) => img.getAttribute('src'));
    expect(imageSrcs).toEqual([mediaUrl, mediaUrl]);
    expect(imageSrcs).not.toContain('');
  });

  it('does not pick the raw CDN url when the server reports cdn_signed (MUL-3254)', () => {
    configStore.setState({ cdnDomain: 'cdn.example.test', cdnSigned: true });
    const id = '11111111-2222-3333-4444-555555555555';
    const markdownUrl = `https://api.goosar.ru/api/attachments/${id}/download`;
    const att = makeRecord({
      id,
      url: 'https://cdn.example.test/uploads/ws/shot.png',
      markdown_url: markdownUrl,
      download_url: '',
    });
    resolverState.attachments = [att];

    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: markdownUrl,
          filename: 'shot.png',
          forceKind: 'image',
        }}
      />,
    );

    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe(markdownUrl);
    expect(getAttachmentMock).not.toHaveBeenCalled();
  });

  it('re-signs the inline media URL through getAttachment on token-mode clients (MUL-3254)', async () => {
    getBaseUrlMock.mockReturnValue('https://api.goosar.ru');
    configStore.setState({ cdnDomain: 'cdn.example.test', cdnSigned: true });
    const id = '11111111-2222-3333-4444-555555555555';
    const markdownUrl = `https://api.goosar.ru/api/attachments/${id}/download`;
    const signed = 'https://cdn.example.test/uploads/ws/shot.png?Signature=fresh&Key-Pair-Id=K';
    const att = makeRecord({
      id,
      url: 'https://cdn.example.test/uploads/ws/shot.png',
      markdown_url: markdownUrl,
      download_url: '',
    });
    resolverState.attachments = [att];
    getAttachmentMock.mockResolvedValue(makeRecord({ id, download_url: signed }));

    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: markdownUrl,
          filename: 'shot.png',
          forceKind: 'image',
        }}
      />,
    );

    await waitFor(() => {
      expect(document.querySelector('img')?.getAttribute('src')).toBe(signed);
    });
    expect(getAttachmentMock).toHaveBeenCalledWith(id);
  });

  it('re-signs URL-only inline media when no resolver record is available (MUL-3254)', async () => {
    getBaseUrlMock.mockReturnValue('https://api.goosar.ru');
    configStore.setState({ cdnDomain: 'cdn.example.test', cdnSigned: true });
    const id = '11111111-2222-3333-4444-555555555555';
    const markdownUrl = `https://api.goosar.ru/api/attachments/${id}/download`;
    const signed = 'https://cdn.example.test/uploads/ws/shot.png?Signature=fresh&Key-Pair-Id=K';
    getAttachmentMock.mockResolvedValue(makeRecord({ id, download_url: signed }));

    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: markdownUrl,
          filename: 'shot.png',
          forceKind: 'image',
        }}
      />,
    );

    await waitFor(() => {
      expect(document.querySelector('img')?.getAttribute('src')).toBe(signed);
    });
    expect(getAttachmentMock).toHaveBeenCalledWith(id);
  });

  it('keeps the picked URL when fresh metadata has no signed download_url (MUL-3254)', async () => {
    getBaseUrlMock.mockReturnValue('https://api.goosar.ru');
    const id = '11111111-2222-3333-4444-555555555555';
    const markdownUrl = `https://api.goosar.ru/api/attachments/${id}/download`;
    const att = makeRecord({
      id,
      url: 'https://cdn.example.test/uploads/ws/shot.png',
      markdown_url: markdownUrl,
      download_url: '',
    });
    configStore.setState({ cdnDomain: '', cdnSigned: false });
    resolverState.attachments = [att];
    getAttachmentMock.mockResolvedValue(
      makeRecord({ id, download_url: `/api/attachments/${id}/download` }),
    );

    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: markdownUrl,
          filename: 'shot.png',
          forceKind: 'image',
        }}
      />,
    );

    await waitFor(() => expect(getAttachmentMock).toHaveBeenCalledWith(id));
    expect(document.querySelector('img')?.getAttribute('src')).toBe(markdownUrl);
  });

  it('forceKind=image renders as image even when filename is empty (markdown ![](url) regression)', () => {
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: 'https://external.example/no-ext-here',
          filename: '',
          forceKind: 'image',
        }}
      />,
    );
    expect(document.querySelector('img')).toBeTruthy();
    expect(screen.queryByText('Uploading')).toBeNull();
  });

  it('external image (no resolver match) renders <img> and falls back to openByUrl on Download', () => {
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: 'https://external.example/foo.png',
          filename: 'foo.png',
        }}
      />,
    );
    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe('https://external.example/foo.png');
    fireEvent.click(screen.getByTitle('Download'));
    expect(openByUrlMock).toHaveBeenCalledWith('https://external.example/foo.png');
    expect(downloadMock).not.toHaveBeenCalled();
  });

  it('uploading image renders no toolbar (loader state)', () => {
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: 'blob://local',
          filename: 'in-flight.png',
          uploading: true,
        }}
      />,
    );
    expect(screen.queryByTitle('View')).toBeNull();
    expect(screen.queryByTitle('Download')).toBeNull();
  });

  it('stable /api/attachments/<id>/download download_url falls through to the durable record.markdown_url for native <img> loadability (MUL-3192)', () => {
    const att = makeRecord({
      url: 'https://prod.s3.amazonaws.com/key.png',
      markdown_url: 'https://api.goosar.test/api/attachments/att-1/download',
      download_url: '/api/attachments/att-1/download',
    });
    renderWithQuery(<Attachment attachment={{ kind: 'record', attachment: att }} />);
    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe('https://api.goosar.test/api/attachments/att-1/download');
    expect(img?.getAttribute('src')).not.toContain('prod.s3.amazonaws.com');
  });

  it('legacy backend (no markdown_url on record) still falls back to record.url', () => {
    const att = makeRecord({
      url: 'https://cdn.example.test/legacy.png',
      markdown_url: '',
      download_url: '/api/attachments/att-1/download',
    });
    renderWithQuery(<Attachment attachment={{ kind: 'record', attachment: att }} />);
    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe('https://cdn.example.test/legacy.png');
  });
});

describe('Attachment — html dispatch', () => {
  it('record html with attachmentId renders HtmlAttachmentPreview (no file-card chrome)', () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: '<p>chart</p>',
      originalContentType: 'text/html',
    });
    const att = makeRecord({
      filename: 'report.html',
      content_type: 'text/html',
      url: 'https://cdn.example.test/report.html',
    });
    renderWithQuery(<Attachment attachment={{ kind: 'record', attachment: att }} />);
    expect(screen.queryByText('report.html')).toBeNull();
    expect(screen.getByTitle('Preview')).toBeTruthy();
    expect(screen.getByTitle('Download')).toBeTruthy();
  });

  it('url-only html (no resolver match) falls back to AttachmentCard chrome', () => {
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: 'https://external.example/report.html',
          filename: 'report.html',
          contentType: 'text/html',
        }}
      />,
    );
    expect(screen.getByText('report.html')).toBeTruthy();
    expect(document.querySelector('iframe')).toBeNull();
  });
});

describe('Attachment — file-card dispatch', () => {
  it('record pdf renders the file-card chrome (filename + Preview/Download)', () => {
    const att = makeRecord({
      filename: 'manual.pdf',
      content_type: 'application/pdf',
    });
    renderWithQuery(<Attachment attachment={{ kind: 'record', attachment: att }} />);
    expect(screen.getByText('manual.pdf')).toBeTruthy();
    expect(document.querySelector('iframe')).toBeNull();
    expect(document.querySelector('img')).toBeNull();
  });

  it('url-only stable attachment download file-card resolves to record and downloads by id', () => {
    const id = '11111111-2222-3333-4444-555555555555';
    const href = `/api/attachments/${id}/download`;
    resolverState.attachments = [
      makeRecord({
        id,
        filename: 'manual.pdf',
        content_type: 'application/pdf',
        url: '/uploads/manual.pdf',
        markdown_url: href,
        download_url: href,
      }),
    ];

    renderWithQuery(<Attachment attachment={{ kind: 'url', url: href, filename: 'manual.pdf' }} />);

    expect(screen.getByText('manual.pdf')).toBeTruthy();
    fireEvent.mouseDown(screen.getByTitle('Download'));
    expect(downloadMock).toHaveBeenCalledWith(id);
  });

  it('uploading file-card surfaces the uploading template, no Preview/Download', () => {
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: 'blob://local',
          filename: 'in-flight.zip',
          uploading: true,
        }}
      />,
    );
    expect(screen.getByText('Uploading {{filename}}')).toBeTruthy();
    expect(screen.queryByTitle('Preview')).toBeNull();
    expect(screen.queryByTitle('Download')).toBeNull();
  });
});

describe('Attachment — absolutize site-relative URLs (MUL-3192)', () => {
  it('prefixes site-relative /api/attachments/<id>/download with apiBaseUrl in Desktop-like environments', () => {
    getBaseUrlMock.mockReturnValue('https://api.example.test');
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: '/api/attachments/abc-1/download',
          filename: 'shot.png',
          forceKind: 'image',
        }}
      />,
    );
    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe(
      'https://api.example.test/api/attachments/abc-1/download',
    );
  });

  it('absolutizes the legacy site-relative /uploads/<key> when record.markdown_url is empty (legacy backend)', () => {
    getBaseUrlMock.mockReturnValue('https://api.example.test');
    const att = makeRecord({
      url: '/uploads/ws-1/abc.png',
      markdown_url: '',
      download_url: '/api/attachments/att-1/download',
    });
    renderWithQuery(<Attachment attachment={{ kind: 'record', attachment: att }} />);
    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe('https://api.example.test/uploads/ws-1/abc.png');
  });

  it('strips a trailing slash on apiBaseUrl so the prefixed URL has exactly one separator', () => {
    getBaseUrlMock.mockReturnValue('https://api.example.test/');
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: '/api/attachments/abc-2/download',
          filename: 'shot.png',
          forceKind: 'image',
        }}
      />,
    );
    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe(
      'https://api.example.test/api/attachments/abc-2/download',
    );
  });

  it('leaves absolute https URLs untouched even when apiBaseUrl is set', () => {
    getBaseUrlMock.mockReturnValue('https://api.example.test');
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: 'https://cdn.other.test/foo.png',
          filename: 'foo.png',
          forceKind: 'image',
        }}
      />,
    );
    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe('https://cdn.other.test/foo.png');
  });

  it('leaves blob: URLs untouched (in-flight upload preview)', () => {
    getBaseUrlMock.mockReturnValue('https://api.example.test');
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: 'blob:https://app.local/abc-123',
          filename: 'in-flight.png',
          forceKind: 'image',
          // uploading=true would hide the toolbar, but the src normalization
          // path runs regardless — keep it false so we still mount the <img>.
        }}
      />,
    );
    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe('blob:https://app.local/abc-123');
  });

  it('is a no-op when apiBaseUrl is empty (web app same-origin proxy)', () => {
    getBaseUrlMock.mockReturnValue('');
    renderWithQuery(
      <Attachment
        attachment={{
          kind: 'url',
          url: '/api/attachments/abc-3/download',
          filename: 'shot.png',
          forceKind: 'image',
        }}
      />,
    );
    const img = document.querySelector('img');
    expect(img?.getAttribute('src')).toBe('/api/attachments/abc-3/download');
  });
});

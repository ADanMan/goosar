import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

const { getAttachmentTextContentMock } = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
}));

vi.mock('@goosar/core/api', () => ({
  api: { getAttachmentTextContent: getAttachmentTextContentMock },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock('../i18n', () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        image: { download: 'Download' },
        attachment: {
          preview: 'Preview',
          preview_loading: 'Loading preview…',
          preview_failed: "Couldn't load preview",
          open_in_new_tab: 'Open in new tab',
        },
      }),
  }),
}));

const { openInNewTabMock, getShareableUrlMock, navState } = vi.hoisted(() => ({
  openInNewTabMock: vi.fn(),
  getShareableUrlMock: vi.fn((p: string) => `https://app.example${p}`),
  navState: { hasOpenInNewTab: true },
}));

vi.mock('../navigation', () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: '/acme/issues',
    searchParams: new URLSearchParams(),
    ...(navState.hasOpenInNewTab ? { openInNewTab: openInNewTabMock } : {}),
    getShareableUrl: getShareableUrlMock,
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

import { HtmlAttachmentPreview } from './html-attachment-preview';

function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  navState.hasOpenInNewTab = true;
});
afterEach(() => vi.restoreAllMocks());

describe('HtmlAttachmentPreview — visual shell (does not use file-card chrome)', () => {
  it('does not render the filename row that AttachmentCard chrome would render', async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: '<p>ok</p>',
      originalContentType: 'text/html',
    });
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => {
      expect(document.querySelector('iframe')).toBeTruthy();
    });
    expect(screen.queryByText('report.html')).toBeNull();
  });

  it("renders iframe with sandbox='allow-scripts' and srcdoc when text loads", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: '<p>chart goes here</p>',
      originalContentType: 'text/html',
    });
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => {
      const frame = document.querySelector('iframe') as HTMLIFrameElement | null;
      expect(frame).toBeTruthy();
      expect(frame?.getAttribute('sandbox')).toBe('allow-scripts');
      const srcdoc = frame?.getAttribute('srcdoc') ?? '';
      expect(srcdoc.startsWith('<p>chart goes here</p>')).toBe(true);
      expect(srcdoc).toContain('scrollIntoView');
    });
  });
});

describe('HtmlAttachmentPreview — toolbar actions', () => {
  it('invokes onPreview when Maximize is clicked', async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: '<p>ok</p>',
      originalContentType: 'text/html',
    });
    const onPreview = vi.fn();
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={onPreview}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => expect(screen.getByTitle('Preview')).toBeTruthy());
    fireEvent.mouseDown(screen.getByTitle('Preview'));
    expect(onPreview).toHaveBeenCalled();
  });

  it('invokes onDownload when Download is clicked', async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: '<p>ok</p>',
      originalContentType: 'text/html',
    });
    const onDownload = vi.fn();
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={() => {}}
        onDownload={onDownload}
      />,
    );
    await waitFor(() => expect(screen.getByTitle('Download')).toBeTruthy());
    fireEvent.mouseDown(screen.getByTitle('Download'));
    expect(onDownload).toHaveBeenCalled();
  });

  it('does not render a Copy code button — attachments are files, not source snippets', async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: '<p>ok</p>',
      originalContentType: 'text/html',
    });
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => expect(document.querySelector('iframe')).toBeTruthy());
    expect(screen.queryByTitle('Copy code')).toBeNull();
  });

  it('invokes navigation.openInNewTab with the preview path when available (desktop)', async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: '<p>ok</p>',
      originalContentType: 'text/html',
    });
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => expect(screen.getByTitle('Open in new tab')).toBeTruthy());
    fireEvent.mouseDown(screen.getByTitle('Open in new tab'));
    expect(openInNewTabMock).toHaveBeenCalledWith(
      '/acme/attachments/att-1/preview?name=report.html',
      'report.html',
      { activate: true },
    );
  });

  it('falls back to window.open against the shareable URL when openInNewTab is absent (web)', async () => {
    navState.hasOpenInNewTab = false;
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: '<p>ok</p>',
      originalContentType: 'text/html',
    });
    const windowOpenSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => expect(screen.getByTitle('Open in new tab')).toBeTruthy());
    fireEvent.mouseDown(screen.getByTitle('Open in new tab'));
    expect(openInNewTabMock).not.toHaveBeenCalled();
    expect(windowOpenSpy).toHaveBeenCalledWith(
      'https://app.example/acme/attachments/att-1/preview?name=report.html',
      '_blank',
      'noopener,noreferrer',
    );
  });
});

describe('HtmlAttachmentPreview — failure mode does not unmount the toolbar', () => {
  it('keeps Preview and Download enabled when fetch errors', async () => {
    getAttachmentTextContentMock.mockRejectedValueOnce(new Error('nope'));
    const onPreview = vi.fn();
    const onDownload = vi.fn();
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={onPreview}
        onDownload={onDownload}
      />,
    );
    await waitFor(() => {
      expect(screen.getByTestId('html-attachment-preview-error')).toBeTruthy();
    });
    expect(document.querySelector('iframe')).toBeNull();
    expect(screen.queryByText('report.html')).toBeNull();

    const previewBtn = screen.getByTitle('Preview') as HTMLButtonElement;
    const downloadBtn = screen.getByTitle('Download') as HTMLButtonElement;
    const openInNewTabBtn = screen.getByTitle('Open in new tab') as HTMLButtonElement;
    expect(previewBtn.disabled).toBe(false);
    expect(downloadBtn.disabled).toBe(false);
    expect(openInNewTabBtn.disabled).toBe(false);

    fireEvent.mouseDown(previewBtn);
    expect(onPreview).toHaveBeenCalled();
    fireEvent.mouseDown(downloadBtn);
    expect(onDownload).toHaveBeenCalled();
  });
});

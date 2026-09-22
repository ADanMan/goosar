import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

const { getAttachmentTextContentMock } = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
}));

vi.mock('@goosar/core/api', () => ({
  api: {
    getAttachmentTextContent: getAttachmentTextContentMock,
    getAttachment: vi.fn(),
  },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock('../../navigation', () => ({
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
  };
});

import { AttachmentList } from './comment-card';

function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

describe('AttachmentList — standalone HTML attachment routes through AttachmentBlock', () => {
  it('renders an iframe (no file-card chrome) for a standalone HTML attachment', async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: '<p>chart</p>',
      originalContentType: 'text/html',
    });
    const attachment = {
      id: 'att-1',
      url: '/uploads/report.html',
      filename: 'report.html',
      content_type: 'text/html',
      size_bytes: 0,
    } as any;

    renderWithQuery(<AttachmentList attachments={[attachment]} content="" />);

    const frame = await waitFor(() => {
      const f = document.querySelector('iframe') as HTMLIFrameElement | null;
      expect(f).toBeTruthy();
      return f!;
    });
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts');
    expect(frame.getAttribute('srcdoc')).toContain('<p>chart</p>');
    expect(screen.queryByText('report.html')).toBeNull();
  });
});

describe('AttachmentList — inline attachment filtering', () => {
  it('does not render a bottom attachment row when the body already has the stable file-card URL', () => {
    const id = '11111111-2222-3333-4444-555555555555';
    const href = `/api/attachments/${id}/download`;
    const attachment = {
      id,
      url: '/uploads/report.pdf',
      filename: 'report.pdf',
      content_type: 'application/pdf',
      size_bytes: 1024,
    } as any;

    const { container } = renderWithQuery(
      <AttachmentList attachments={[attachment]} content={`!file[report.pdf](${href})`} />,
    );

    expect(screen.queryByText('report.pdf')).toBeNull();
    expect(container.firstChild).toBeNull();
  });

  it('does not render a bottom attachment row when the body already has the response download_url', () => {
    const href = 'https://cdn.example.test/report.pdf?Signature=stale';
    const attachment = {
      id: '11111111-2222-3333-4444-555555555555',
      url: '/uploads/report.pdf',
      download_url: 'https://cdn.example.test/report.pdf?Signature=fresh',
      filename: 'report.pdf',
      content_type: 'application/pdf',
      size_bytes: 1024,
    } as any;

    const { container } = renderWithQuery(
      <AttachmentList attachments={[attachment]} content={`!file[report.pdf](${href})`} />,
    );

    expect(screen.queryByText('report.pdf')).toBeNull();
    expect(container.firstChild).toBeNull();
  });
});

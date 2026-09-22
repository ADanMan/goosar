import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

vi.mock('../i18n', () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        image: { download: 'Download' },
        attachment: {
          preview: 'Preview',
          preview_loading: 'Loading preview…',
        },
        file_card: { uploading: 'Uploading {{filename}}' },
      }),
  }),
}));

import { AttachmentCard } from './attachment-card';

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

describe('AttachmentCard — chrome row', () => {
  it('renders chrome only and never an inline iframe (HTML rich preview lives in HtmlAttachmentPreview)', () => {
    render(
      <AttachmentCard
        filename="report.html"
        contentType="text/html"
        attachmentId="att-1"
        href="https://cdn.example/report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.getByText('report.html')).toBeTruthy();
    expect(document.querySelector('iframe')).toBeNull();
  });

  it("hides the Eye button for an html URL-only source (the modal's /content proxy is ID-keyed)", () => {
    render(
      <AttachmentCard
        filename="report.html"
        contentType="text/html"
        href="https://cdn.example/report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.queryByTitle('Preview')).toBeNull();
    expect(screen.getByTitle('Download')).toBeTruthy();
  });

  it('shows the Eye button for an html source when an attachmentId is available', () => {
    render(
      <AttachmentCard
        filename="report.html"
        contentType="text/html"
        attachmentId="att-1"
        href="https://cdn.example/report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.getByTitle('Preview')).toBeTruthy();
  });

  it('shows the Eye button for a URL-only pdf source (modal renders pdfs directly from URL)', () => {
    render(
      <AttachmentCard
        filename="manual.pdf"
        contentType="application/pdf"
        href="https://cdn.example/manual.pdf"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.getByTitle('Preview')).toBeTruthy();
  });
});

describe('AttachmentCard — Eye / Download buttons', () => {
  it('invokes onPreview when Eye is clicked', () => {
    const onPreview = vi.fn();
    render(
      <AttachmentCard
        filename="manual.pdf"
        contentType="application/pdf"
        attachmentId="att-1"
        href="https://cdn.example/manual.pdf"
        onPreview={onPreview}
        onDownload={() => {}}
      />,
    );
    fireEvent.mouseDown(screen.getByTitle('Preview'));
    expect(onPreview).toHaveBeenCalled();
  });

  it('invokes onDownload when Download is clicked', () => {
    const onDownload = vi.fn();
    render(
      <AttachmentCard
        filename="manual.pdf"
        contentType="application/pdf"
        attachmentId="att-1"
        href="https://cdn.example/manual.pdf"
        onPreview={() => {}}
        onDownload={onDownload}
      />,
    );
    fireEvent.mouseDown(screen.getByTitle('Download'));
    expect(onDownload).toHaveBeenCalled();
  });

  it('hides Eye and Download buttons while uploading', () => {
    render(
      <AttachmentCard
        filename="report.html"
        contentType="text/html"
        attachmentId="att-1"
        href="https://cdn.example/report.html"
        uploading
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.queryByTitle('Preview')).toBeNull();
    expect(screen.queryByTitle('Download')).toBeNull();
    expect(screen.getByText('Uploading {{filename}}')).toBeTruthy();
  });
});

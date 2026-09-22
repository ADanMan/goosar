import { describe, it, expect, beforeAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';
import { renderWithI18n } from '../../test/i18n';
import { WebhookPayloadPreview } from './webhook-payload-preview';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

beforeAll(() => {
  Object.assign(navigator, {
    clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
  });
});

const envelope = (event: string, eventPayload: unknown, extras: Record<string, unknown> = {}) => ({
  event,
  eventPayload,
  request: { receivedAt: '2026-05-13T12:34:56Z', contentType: 'application/json', ...extras },
});

describe('WebhookPayloadPreview', () => {
  it('renders the envelope event in the header', () => {
    renderWithI18n(
      <WebhookPayloadPreview
        payload={envelope('github.pull_request.opened', { number: 1 })}
        defaultOpen
      />,
    );
    expect(screen.getByText('github.pull_request.opened')).toBeInTheDocument();
  });

  it('falls back gracefully when payload is not an envelope', () => {
    renderWithI18n(<WebhookPayloadPreview payload={{ hello: 'world' }} defaultOpen />);
    expect(screen.getByText(/hello/)).toBeInTheDocument();
  });

  it('truncates display when the payload exceeds 4 KiB but copies full text', async () => {
    const bigPayload = envelope('demo.big', { blob: 'x'.repeat(5 * 1024) });
    renderWithI18n(<WebhookPayloadPreview payload={bigPayload} defaultOpen />);
    expect(screen.getByText(/truncated/i)).toBeInTheDocument();

    const pre = document.querySelector('pre');
    expect(pre).not.toBeNull();
    expect((pre!.textContent ?? '').length).toBeLessThan(5 * 1024 + 200);

    fireEvent.click(screen.getByRole('button', { name: /copy/i }));
    const writeText = navigator.clipboard.writeText as ReturnType<typeof vi.fn>;
    expect(writeText).toHaveBeenCalled();
    const lastCall = writeText.mock.calls[writeText.mock.calls.length - 1];
    if (!lastCall) throw new Error('clipboard.writeText was not called');
    const written = lastCall[0] as string;
    expect(written.length).toBeGreaterThan(5 * 1024);
    expect(written).toContain('xxxxxxxx');
  });
});

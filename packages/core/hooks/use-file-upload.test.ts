/**
 * @vitest-environment jsdom
 */
import { describe, expect, it, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import type { ApiClient } from '../api/client';
import type { Attachment } from '../types';
import { useFileUpload, type UploadResult } from './use-file-upload';

function makeAttachment(overrides: Partial<Attachment> = {}): Attachment {
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
    url: '/uploads/ws-1/shot.png',
    download_url: '/api/attachments/att-1/download',
    markdown_url: 'https://api.goosar.test/api/attachments/att-1/download',
    content_type: 'image/png',
    size_bytes: 1,
    created_at: '2026-06-10T00:00:00Z',
    ...overrides,
  };
}

function makeApi(att: Attachment): ApiClient {
  return {
    uploadFile: vi.fn().mockResolvedValue(att),
  } as unknown as ApiClient;
}

async function runUpload(api: ApiClient): Promise<UploadResult | null> {
  const { result } = renderHook(() => useFileUpload(api));
  let upload: UploadResult | null = null;
  await act(async () => {
    upload = await result.current.upload(new File(['data'], 'shot.png', { type: 'image/png' }));
  });
  return upload;
}

describe('useFileUpload — markdownLink picks the durable URL with three-layer fallback', () => {
  it('prefers att.markdown_url when the server populates it (modern deployment)', async () => {
    const att = makeAttachment({
      markdown_url: 'https://cdn.goosar.test/uploads/abc.png',
    });
    const upload = await runUpload(makeApi(att));
    expect(upload?.markdownLink).toBe('https://cdn.goosar.test/uploads/abc.png');
    expect(upload?.link).toBe(att.url);
  });

  it('falls back to the site-relative download path when the server omitted markdown_url (legacy server)', async () => {
    const att = makeAttachment({ markdown_url: '' });
    const upload = await runUpload(makeApi(att));
    expect(upload?.markdownLink).toBe('/api/attachments/att-1/download');
  });

  it("falls back to att.url when there's no attachment-row id (no-workspace avatar branch)", async () => {
    const att = makeAttachment({
      id: '',
      markdown_url: '',
      url: 'https://cdn.goosar.test/avatars/u-1.png',
    });
    const upload = await runUpload(makeApi(att));
    expect(upload?.markdownLink).toBe('https://cdn.goosar.test/avatars/u-1.png');
    expect(upload?.link).toBe('https://cdn.goosar.test/avatars/u-1.png');
  });

  it('rejects oversize files before hitting the network', async () => {
    const att = makeAttachment();
    const api = makeApi(att);
    const huge = new File([new ArrayBuffer(1)], 'big.bin', {
      type: 'application/octet-stream',
    });
    Object.defineProperty(huge, 'size', { value: 200 * 1024 * 1024 });

    const { result } = renderHook(() => useFileUpload(api));
    await expect(
      act(async () => {
        await result.current.upload(huge);
      }),
    ).rejects.toThrow(/100 MB/);
    expect(api.uploadFile as ReturnType<typeof vi.fn>).not.toHaveBeenCalled();
  });
});

describe('useFileUpload — concurrent uploads (MUL-3339 regression)', () => {
  it('keeps uploading=true until ALL concurrent uploads resolve', async () => {
    const att1 = makeAttachment({ id: 'att-1' });
    const att2 = makeAttachment({ id: 'att-2' });
    let resolve1: (v: Attachment) => void = () => {};
    let resolve2: (v: Attachment) => void = () => {};
    const p1 = new Promise<Attachment>((r) => {
      resolve1 = r;
    });
    const p2 = new Promise<Attachment>((r) => {
      resolve2 = r;
    });
    const uploadFile = vi
      .fn<(file: File) => Promise<Attachment>>()
      .mockReturnValueOnce(p1)
      .mockReturnValueOnce(p2);
    const api = { uploadFile } as unknown as ApiClient;

    const { result } = renderHook(() => useFileUpload(api));
    expect(result.current.uploading).toBe(false);

    let pending1: Promise<UploadResult | null> = Promise.resolve(null);
    let pending2: Promise<UploadResult | null> = Promise.resolve(null);
    await act(async () => {
      pending1 = result.current.upload(new File(['1'], 'a.png', { type: 'image/png' }));
      pending2 = result.current.upload(new File(['2'], 'b.png', { type: 'image/png' }));
    });
    expect(result.current.uploading).toBe(true);

    await act(async () => {
      resolve1(att1);
      await pending1;
    });
    expect(result.current.uploading).toBe(true);

    await act(async () => {
      resolve2(att2);
      await pending2;
    });
    expect(result.current.uploading).toBe(false);
  });

  it('decrements correctly when one of the concurrent uploads throws', async () => {
    const att = makeAttachment();
    let resolveOk: (v: Attachment) => void = () => {};
    let rejectBad: (e: Error) => void = () => {};
    const ok = new Promise<Attachment>((r) => {
      resolveOk = r;
    });
    const bad = new Promise<Attachment>((_, j) => {
      rejectBad = j;
    });
    const uploadFile = vi
      .fn<(file: File) => Promise<Attachment>>()
      .mockReturnValueOnce(ok)
      .mockReturnValueOnce(bad);
    const api = { uploadFile } as unknown as ApiClient;

    const { result } = renderHook(() => useFileUpload(api));
    let okPending: Promise<UploadResult | null> = Promise.resolve(null);
    let badPending: Promise<UploadResult | null> = Promise.resolve(null);
    await act(async () => {
      okPending = result.current.upload(new File(['a'], 'a.png', { type: 'image/png' }));
      badPending = result.current
        .upload(new File(['b'], 'b.png', { type: 'image/png' }))
        .catch(() => null);
    });
    expect(result.current.uploading).toBe(true);

    await act(async () => {
      rejectBad(new Error('boom'));
      await badPending;
    });
    expect(result.current.uploading).toBe(true);

    await act(async () => {
      resolveOk(att);
      await okPending;
    });
    expect(result.current.uploading).toBe(false);
  });
});

// @vitest-environment jsdom
import { useEffect, useLayoutEffect, useMemo, useRef, type ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { act, render } from '@testing-library/react';
import { I18nProvider } from '@goosar/core/i18n/react';
import type { DraftUpload } from '@goosar/core/drafts';
import type { UploadResult } from '@goosar/core/hooks/use-file-upload';
import type { Attachment } from '@goosar/core/types';
import enCommon from '../locales/en/common.json';
import enEditor from '../locales/en/editor.json';
import { markPastedTextFile, PASTED_TEXT_FILENAME } from './extensions/file-upload';
import type { ContentEditorRef } from './content-editor';
import type { UploadGate } from './use-upload-gate';
import {
  useCoordinatedUploads,
  __liveEditorRegistryKeysForTest,
  type UploadDraftBinding,
} from './use-coordinated-uploads';

const mockApiUploadFile = vi.hoisted(() => vi.fn());
vi.mock('@goosar/core/api', () => ({ api: { uploadFile: mockApiUploadFile } }));
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

const TEST_RESOURCES = { en: { common: enCommon, editor: enEditor } };

const inertGate: UploadGate = {
  uploading: false,
  onUploadingChange: () => {},
  isBlocked: () => false,
};

function makeBinding(key: string): UploadDraftBinding {
  return {
    registryKey: key,
    getUploads: () => [],
    addUpload: () => {},
    settleUpload: () => {},
    failUpload: () => {},
    removeUpload: () => {},
    getBody: () => '',
    appendToBody: () => {},
  };
}

function HookHost({ registryKey }: { registryKey: string }) {
  const editorRef = useRef<ContentEditorRef | null>(null);
  const binding = useMemo(() => makeBinding(registryKey), [registryKey]);
  useCoordinatedUploads(binding, [], {}, inertGate, editorRef);
  return null;
}

function CaptureAfterCommit({
  registryKey,
  capture,
  children,
}: {
  registryKey: string;
  capture: (keys: string[]) => void;
  children: ReactNode;
}) {
  useLayoutEffect(() => {
    capture(__liveEditorRegistryKeysForTest());
  }, [registryKey, capture]);
  return <>{children}</>;
}

function Probe({
  registryKey,
  capture,
}: {
  registryKey: string;
  capture: (keys: string[]) => void;
}) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CaptureAfterCommit registryKey={registryKey} capture={capture}>
        <HookHost registryKey={registryKey} />
      </CaptureAfterCommit>
    </I18nProvider>
  );
}

describe('useCoordinatedUploads live-editor registry timing', () => {
  it('registers within the commit (layout), so a key switch is visible before any task runs', () => {
    const captures: string[][] = [];
    const capture = (keys: string[]) => captures.push(keys);

    const view = render(<Probe registryKey="probe:a" capture={capture} />);
    expect(captures.at(-1)).toContain('probe:a');

    view.rerender(<Probe registryKey="probe:b" capture={capture} />);

    expect(captures.at(-1)).toContain('probe:b');
    expect(captures.at(-1)).not.toContain('probe:a');

    view.unmount();
    expect(__liveEditorRegistryKeysForTest()).not.toContain('probe:b');
  });
});

function makeRecordingBinding(key: string) {
  const state = { uploads: [] as DraftUpload[], body: '' };
  const binding: UploadDraftBinding = {
    registryKey: key,
    getUploads: () => state.uploads,
    addUpload: (upload) => {
      state.uploads = [...state.uploads, upload];
    },
    settleUpload: () => {},
    failUpload: () => {},
    removeUpload: (clientUploadId) => {
      state.uploads = state.uploads.filter((u) => u.clientUploadId !== clientUploadId);
    },
    getBody: () => state.body,
    appendToBody: (markdown) => {
      state.body = state.body ? `${state.body}\n\n${markdown}` : markdown;
    },
  };
  return { binding, state };
}

function UploadHost({
  binding,
  expose,
}: {
  binding: UploadDraftBinding;
  expose: (upload: (file: File) => Promise<UploadResult | null>) => void;
}) {
  const editorRef = useRef<ContentEditorRef | null>(null);
  const { handleUpload } = useCoordinatedUploads(binding, [], {}, inertGate, editorRef);
  useEffect(() => {
    expose(handleUpload);
  }, [handleUpload, expose]);
  return null;
}

describe('useCoordinatedUploads paste-as-file recovery', () => {
  it("puts a failed paste's source text back into the persisted body after the mount dies", async () => {
    const pastedText = 'a long pasted wall of text that became an attachment';
    const { binding, state } = makeRecordingBinding('probe:paste');
    let upload!: (file: File) => Promise<UploadResult | null>;

    let rejectUpload!: (err: Error) => void;
    mockApiUploadFile.mockImplementationOnce(
      () =>
        new Promise<Attachment>((_resolve, reject) => {
          rejectUpload = reject;
        }),
    );

    const view = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <UploadHost
          binding={binding}
          expose={(fn) => {
            upload = fn;
          }}
        />
      </I18nProvider>,
    );

    await act(async () => {
      void upload(
        markPastedTextFile(
          new File([pastedText], PASTED_TEXT_FILENAME, { type: 'text/plain' }),
          pastedText,
        ),
      );
    });
    expect(state.uploads).toHaveLength(1);

    view.unmount();
    await act(async () => {
      rejectUpload(new Error('upload failed'));
      await Promise.resolve();
    });

    expect(state.body).toContain(pastedText);
    expect(state.uploads).toHaveLength(0);
  });
});

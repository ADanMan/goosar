import type { ApiClient } from '../api/client';
import type { Attachment } from '../types';
import { createLogger } from '../logger';

const logger = createLogger('drafts.upload-coordinator');

export interface UploadCoordinatorContext {
  issueId?: string;
  commentId?: string;
  chatSessionId?: string;
}

export type UploadOutcome =
  | { clientUploadId: string; status: 'uploaded'; attachment: Attachment }
  | { clientUploadId: string; status: 'failed'; error: Error };

export interface StartUploadArgs {
  clientUploadId: string;
  file: File;
  api: Pick<ApiClient, 'uploadFile'>;
  ctx?: UploadCoordinatorContext;
  onSettled: (outcome: UploadOutcome) => void;
}

const controllers = new Map<string, AbortController>();

export function startUpload({ clientUploadId, file, api, ctx, onSettled }: StartUploadArgs): void {
  const controller = new AbortController();
  controllers.set(clientUploadId, controller);

  void (async () => {
    try {
      const attachment = await api.uploadFile(
        file,
        {
          issueId: ctx?.issueId,
          commentId: ctx?.commentId,
          chatSessionId: ctx?.chatSessionId,
        },
        controller.signal,
      );
      onSettled({ clientUploadId, status: 'uploaded', attachment });
    } catch (err) {
      if (controller.signal.aborted || (err instanceof Error && err.name === 'AbortError')) {
        logger.info('upload aborted', { clientUploadId });
        return;
      }
      onSettled({
        clientUploadId,
        status: 'failed',
        error: err instanceof Error ? err : new Error('Upload failed'),
      });
    } finally {
      if (controllers.get(clientUploadId) === controller) {
        controllers.delete(clientUploadId);
      }
    }
  })();
}

export function abortUpload(clientUploadId: string): void {
  const controller = controllers.get(clientUploadId);
  if (!controller) return;
  controllers.delete(clientUploadId);
  controller.abort();
}

export function abortAll(): void {
  if (controllers.size === 0) return;
  logger.info('aborting all uploads', { count: controllers.size });
  for (const controller of controllers.values()) {
    controller.abort();
  }
  controllers.clear();
}

export function __trackedUploadCountForTest(): number {
  return controllers.size;
}

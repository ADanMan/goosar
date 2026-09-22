'use client';

import { useCallback } from 'react';
import { toast } from 'sonner';
import { api } from '@goosar/core/api';
import { useWorkspaceSlug } from '@goosar/core/paths';
import { resolvePublicFileUrl } from '@goosar/core/workspace/avatar-url';
import { useT } from '../i18n';

interface DesktopBridge {
  downloadURL?: (u: string) => Promise<void> | void;
}

function attachmentDownloadEndpoint(attachmentId: string, workspaceSlug: string): string {
  const params = new URLSearchParams({ workspace_slug: workspaceSlug });
  const path = `/api/attachments/${encodeURIComponent(attachmentId)}/download`;
  const endpoint = `${path}?${params.toString()}`;
  return resolvePublicFileUrl(endpoint) ?? endpoint;
}

function ticketedDownloadUrl(downloadTicketUrl: string | undefined): string | null {
  if (!downloadTicketUrl) return null;
  return resolvePublicFileUrl(downloadTicketUrl);
}

function triggerBrowserDownload(url: string): void {
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = '';
  anchor.rel = 'noopener';
  anchor.style.display = 'none';
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

function hasDesktopDownloadBridge(): boolean {
  if (typeof window === 'undefined') return false;
  const bridge = (window as unknown as { desktopAPI?: DesktopBridge }).desktopAPI;
  return Boolean(bridge?.downloadURL);
}

export function useDownloadAttachment(): (attachmentId: string) => Promise<void> {
  const { t } = useT('editor');
  const workspaceSlug = useWorkspaceSlug();
  return useCallback(
    async (attachmentId: string) => {
      const failed = () => toast.error(t(($) => $.attachment.download_failed));

      if (hasDesktopDownloadBridge()) {
        try {
          const fresh = await api.getAttachment(attachmentId);
          const downloadUrl =
            ticketedDownloadUrl(fresh.download_ticket_url) ??
            resolvePublicFileUrl(fresh.download_url);
          if (!downloadUrl) {
            failed();
            return;
          }
          const bridge = (window as unknown as { desktopAPI?: DesktopBridge }).desktopAPI;
          await bridge!.downloadURL!(downloadUrl);
        } catch {
          failed();
        }
        return;
      }

      try {
        const fresh = await api.getAttachment(attachmentId);
        if (typeof document === 'undefined') {
          failed();
          return;
        }
        const ticketed = ticketedDownloadUrl(fresh.download_ticket_url);
        if (ticketed) {
          triggerBrowserDownload(ticketed);
          return;
        }
        if (!workspaceSlug) {
          failed();
          return;
        }
        triggerBrowserDownload(attachmentDownloadEndpoint(attachmentId, workspaceSlug));
      } catch {
        failed();
      }
    },
    [t, workspaceSlug],
  );
}

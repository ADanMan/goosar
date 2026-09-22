'use client';

import { createContext, use, useMemo, type ReactNode } from 'react';
import type { Attachment } from '@goosar/core/types';
import { attachmentIdFromDownloadURL } from '@goosar/core/types/attachment-url';
import { openExternal } from '../platform';
import { useDownloadAttachment } from './use-download-attachment';

interface ResolvedDownload {
  resolveAttachmentId: (url: string) => string | undefined;
  resolveAttachment: (url: string) => Attachment | undefined;
  openByUrl: (url: string) => void;
}

const AttachmentDownloadContext = createContext<ResolvedDownload | null>(null);

interface ProviderProps {
  attachments?: Attachment[];
  children: ReactNode;
}

function stripQueryAndFragment(url: string): string {
  return url.split(/[?#]/, 1)[0] ?? '';
}

function matchesAttachmentURL(embeddedURL: string, attachmentURL?: string): boolean {
  if (!embeddedURL || !attachmentURL) return false;
  if (embeddedURL === attachmentURL) return true;
  const embeddedStable = stripQueryAndFragment(embeddedURL);
  const attachmentStable = stripQueryAndFragment(attachmentURL);
  return embeddedStable !== '' && embeddedStable === attachmentStable;
}

export function AttachmentDownloadProvider({ attachments, children }: ProviderProps) {
  const download = useDownloadAttachment();
  const value = useMemo<ResolvedDownload>(() => {
    const lookup = (url: string): Attachment | undefined => {
      if (!url || !attachments?.length) return undefined;
      const idFromUrl = attachmentIdFromDownloadURL(url);
      if (idFromUrl) {
        const byId = attachments.find((a) => a.id === idFromUrl);
        if (byId) return byId;
      }
      return attachments.find(
        (a) =>
          matchesAttachmentURL(url, a.url) ||
          matchesAttachmentURL(url, a.download_url) ||
          matchesAttachmentURL(url, a.markdown_url),
      );
    };
    return {
      resolveAttachmentId: (url) => lookup(url)?.id,
      resolveAttachment: lookup,
      openByUrl: (url) => {
        const att = lookup(url);
        if (att) {
          download(att.id);
          return;
        }
        if (url) openExternal(url);
      },
    };
  }, [attachments, download]);
  return (
    <AttachmentDownloadContext.Provider value={value}>
      {children}
    </AttachmentDownloadContext.Provider>
  );
}

export function useAttachmentDownloadResolver(): ResolvedDownload {
  const ctx = use(AttachmentDownloadContext);
  if (ctx) return ctx;
  return {
    resolveAttachmentId: () => undefined,
    resolveAttachment: () => undefined,
    openByUrl: (url) => {
      if (url) openExternal(url);
    },
  };
}

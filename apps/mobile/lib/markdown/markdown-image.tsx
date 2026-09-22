/**
 * Блочное изображение в markdown с реальным соотношением сторон и
 * тапом для открытия в лайтбоксе.
 *
 * Соотношение сторон определяется через Image.getSize; до получения
 * размеров используется плейсхолдер 16:9.
 *
 * Внутренние ссылки на изображения хранятся в схеме `mc://file/<id>`,
 * которую iOS не понимает — URI ищется в списке вложений и подменяется
 * на реальный download_url перед передачей в API изображений.
 * Несовпавшие URI (внешние https-ссылки и известные схемы) грузятся как есть.
 */
import { useEffect, useMemo, useState } from 'react';
import { Image as RNImage, Pressable, View } from 'react-native';
import { Image as ExpoImage } from 'expo-image';
import { useQuery } from '@tanstack/react-query';
import type { Attachment } from '@goosar/core/types';
import { resolveAttachmentUrl } from '@/lib/attachment-url';
import { appConfigOptions } from '@/data/queries/app-config';
import {
  buildTrustedImageHosts,
  isTrustedMarkdownImageUri,
  normalizeExternalImagesMode,
} from './image-policy';
import { BLOCKED_IMAGE_ACCESSIBILITY_LABEL } from './blocked-image-label';
import { useLightbox } from './lightbox-provider';

const API_BASE_URL = process.env.EXPO_PUBLIC_API_URL ?? '';

interface Props {
  uri: string;
  alt?: string;
  attachments?: Attachment[];
}

export function MarkdownImage({ uri, alt, attachments }: Props) {
  const { open } = useLightbox();
  const [aspect, setAspect] = useState<number | null>(null);

  const { data: appConfig } = useQuery(appConfigOptions());
  const mode = normalizeExternalImagesMode(appConfig?.external_images);
  const trustedHosts = useMemo(
    () =>
      buildTrustedImageHosts({
        imageHosts: appConfig?.image_hosts,
        cdnDomain: appConfig?.cdn_domain,
        apiBaseUrl: API_BASE_URL,
      }),
    [appConfig?.image_hosts, appConfig?.cdn_domain],
  );

  const attachmentMatch = useMemo(
    () => attachments?.find((a) => a.url === uri),
    [uri, attachments],
  );

  const blocked =
    attachmentMatch?.download_url == null && !isTrustedMarkdownImageUri(uri, mode, trustedHosts);

  const resolvedUri = useMemo(() => {
    let candidate: string | null | undefined = uri;
    if (attachmentMatch?.download_url) candidate = attachmentMatch.download_url;
    return resolveAttachmentUrl(candidate) ?? uri;
  }, [uri, attachmentMatch]);

  useEffect(() => {
    if (blocked) return;
    let cancelled = false;
    RNImage.getSize(
      resolvedUri,
      (w, h) => {
        if (cancelled || !w || !h) return;
        setAspect(w / h);
      },
      () => {
        if (!cancelled) setAspect(16 / 9);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [resolvedUri, blocked]);

  if (blocked) {
    return (
      <View
        accessible
        accessibilityRole="image"
        accessibilityLabel={alt || BLOCKED_IMAGE_ACCESSIBILITY_LABEL}
        className="rounded-lg overflow-hidden bg-muted"
        style={{ width: '100%', aspectRatio: 16 / 9 }}
      />
    );
  }

  return (
    <Pressable onPress={() => open(resolvedUri)}>
      <View className="rounded-lg overflow-hidden bg-muted">
        <ExpoImage
          source={{ uri: resolvedUri }}
          style={{ width: '100%', aspectRatio: aspect ?? 16 / 9 }}
          contentFit="contain"
          transition={150}
        />
      </View>
    </Pressable>
  );
}

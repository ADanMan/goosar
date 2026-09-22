/**
 * Хук для кнопок изображения/файла в markdown-тулбаре: открывает
 * нужный пикер, при отмене возвращает null, при успехе загружает файл
 * и возвращает { url, filename } для вставки в текст. При ошибке
 * показывает Alert и тоже возвращает null.
 *
 * Флаг uploading позволяет блокировать тулбар на время загрузки.
 */
import { useCallback, useState } from 'react';
import { Alert } from 'react-native';
import * as ImagePicker from 'expo-image-picker';
import * as DocumentPicker from 'expo-document-picker';
import { api, MAX_FILE_SIZE, type FileAsset } from '@/data/api';

export interface FileAttachResult {
  id: string;
  url: string;
  filename: string;
}

export interface UploadContext {
  issueId?: string;
  commentId?: string;
}

interface PickedAsset extends FileAsset {
  size?: number;
}

export function useFileAttach() {
  const [uploading, setUploading] = useState(false);

  const upload = useCallback(
    async (asset: PickedAsset, ctx?: UploadContext): Promise<FileAttachResult | null> => {
      if (asset.size != null && asset.size > MAX_FILE_SIZE) {
        Alert.alert('File too large', 'Files must be smaller than 100 MB.');
        return null;
      }
      setUploading(true);
      try {
        const attachment = await api.uploadFile(asset, ctx);
        return {
          id: attachment.id,
          url: attachment.url,
          filename: attachment.filename,
        };
      } catch (err) {
        Alert.alert('Upload failed', err instanceof Error ? err.message : 'Unknown error');
        return null;
      } finally {
        setUploading(false);
      }
    },
    [],
  );

  const pickAndUploadImage = useCallback(
    async (ctx?: UploadContext): Promise<FileAttachResult | null> => {
      const result = await ImagePicker.launchImageLibraryAsync({
        mediaTypes: ImagePicker.MediaTypeOptions.Images,
        quality: 1,
      });
      if (result.canceled) return null;
      const picked = result.assets[0];
      if (!picked) return null;
      const asset: PickedAsset = {
        uri: picked.uri,
        name: picked.fileName ?? `image-${Date.now()}.jpg`,
        type: picked.mimeType ?? 'image/jpeg',
        size: picked.fileSize,
      };
      return upload(asset, ctx);
    },
    [upload],
  );

  const pickAndUploadFile = useCallback(
    async (ctx?: UploadContext): Promise<FileAttachResult | null> => {
      const result = await DocumentPicker.getDocumentAsync({
        type: '*/*',
        copyToCacheDirectory: true,
      });
      if (result.canceled) return null;
      const picked = result.assets[0];
      if (!picked) return null;
      const asset: PickedAsset = {
        uri: picked.uri,
        name: picked.name,
        type: picked.mimeType ?? 'application/octet-stream',
        size: picked.size,
      };
      return upload(asset, ctx);
    },
    [upload],
  );

  return { pickAndUploadImage, pickAndUploadFile, uploading };
}

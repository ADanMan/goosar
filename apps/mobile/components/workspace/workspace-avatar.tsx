/**
 * Аватар рабочего пространства. Если задан avatar_url — показывает
 * логотип-картинку со скруглёнными углами, иначе — первую букву названия
 * на приглушённой плашке (как в веб-версии). URL прогоняется через
 * resolveAttachmentUrl, так как self-hosted бэкенды отдают относительный
 * путь. Картинка оборачивается во View, потому что NativeWind не умеет
 * применять className к expo-image напрямую.
 */
import { View } from 'react-native';
import { Image as ExpoImage } from 'expo-image';
import { Text } from '@/components/ui/text';
import { resolveAttachmentUrl } from '@/lib/attachment-url';
import { cn } from '@/lib/utils';

export function WorkspaceAvatar({
  name,
  avatarUrl,
  size = 24,
  className,
}: {
  name: string;
  avatarUrl: string | null | undefined;
  size?: number;
  className?: string;
}) {
  const resolved = resolveAttachmentUrl(avatarUrl);
  const borderRadius = Math.round(size / 4);

  if (resolved) {
    return (
      <View
        className={cn('overflow-hidden border border-border', className)}
        style={{ width: size, height: size, borderRadius }}
      >
        <ExpoImage
          source={{ uri: resolved }}
          contentFit="cover"
          accessibilityLabel={name}
          style={{ width: '100%', height: '100%' }}
        />
      </View>
    );
  }

  return (
    <View
      className={cn('items-center justify-center bg-muted border border-border', className)}
      style={{ width: size, height: size, borderRadius }}
    >
      <Text
        className="font-semibold text-muted-foreground"
        style={{ fontSize: Math.round(size * 0.48) }}
      >
        {name.charAt(0).toUpperCase()}
      </Text>
    </View>
  );
}

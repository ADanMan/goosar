import type { ExpoConfig, ConfigContext } from 'expo/config';

export default ({ config }: ConfigContext): ExpoConfig => {
  const env = process.env.APP_ENV ?? 'development';
  const isProd = env === 'production';
  const isStaging = env === 'staging';

  return {
    ...config,
    name: isProd ? 'Goosar' : isStaging ? 'Goosar (Staging)' : 'Goosar (Dev)',
    slug: 'goosar-mobile',
    version: '0.1.0',
    orientation: 'portrait',
    userInterfaceStyle: 'automatic',
    scheme: 'goosar',
    icon: './assets/icon.png',
    ios: {
      supportsTablet: false,
      bundleIdentifier: isProd
        ? (process.env.EXPO_BUNDLE_IDENTIFIER_PROD ?? 'ru.goosar.mobile')
        : isStaging
          ? 'ru.goosar.mobile.staging'
          : (process.env.EXPO_BUNDLE_IDENTIFIER_DEV ?? 'ru.goosar.mobile.dev'),
    },
    plugins: [
      'expo-router',
      'expo-secure-store',
      '@react-native-community/datetimepicker',
      'react-native-enriched-markdown',
      [
        'expo-image-picker',
        {
          photosPermission:
            'Allow Goosar to access your photos to attach images to issues and comments.',
          cameraPermission: false,
          microphonePermission: false,
        },
      ],
      [
        'expo-build-properties',
        {
          ios: {
            buildReactNativeFromSource: true,
          },
        },
      ],
    ],
    extra: { APP_ENV: env },
  };
};

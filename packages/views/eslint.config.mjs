import reactConfig from '@goosar/eslint-config/react';
import i18next from 'eslint-plugin-i18next';

export default [
  ...reactConfig,
  {
    files: ['**/*.tsx'],
    ignores: ['**/*.test.tsx', 'test/**'],
    plugins: { i18next },
    rules: {
      'i18next/no-literal-string': ['error', { mode: 'jsx-text-only' }],
    },
  },
];

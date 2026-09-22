import globals from 'globals';
import reactConfig from '@goosar/eslint-config/react';

export default [
  ...reactConfig,
  {
    ignores: [
      'out/',
      'dist/',
      'resources/bin/',
      'resources-agent/',
      'resources-ca/',
      'resources-mcp-servers/',
      'resources-skills/',
      'resources-playwright/',
      'resources-preset-overlay/',
    ],
  },
  {
    files: ['scripts/**/*.{mjs,js}'],
    languageOptions: {
      globals: { ...globals.node },
    },
  },
  {
    files: ['src/main/**/*.ts'],
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector:
            "CallExpression[callee.object.name='shell'][callee.property.name='openExternal']",
          message:
            "Do not call shell.openExternal directly. Use openExternalSafely from './external-url' so the http/https allowlist stays enforced.",
        },
        {
          selector:
            "CallExpression[callee.object.property.name='webContents'][callee.property.name='downloadURL']",
          message:
            "Do not call webContents.downloadURL directly. Use downloadURLSafely from './external-url' so the http/https allowlist stays enforced.",
        },
      ],
    },
  },
  {
    files: ['src/main/external-url.ts'],
    rules: {
      'no-restricted-syntax': 'off',
    },
  },
  {
    files: ['src/renderer/src/**/*.{ts,tsx}'],
    ignores: ['src/renderer/src/platform/**', 'src/renderer/src/**/*.test.*'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          paths: [
            {
              name: 'react-router-dom',
              importNames: ['useNavigate', 'Navigate'],
              message:
                'Direct navigation from application code breaks the Coordinator protocol (MUL-4741). Use the navigation adapter from src/platform instead.',
            },
          ],
        },
      ],
      'no-restricted-syntax': [
        'error',
        {
          selector: "CallExpression[callee.object.name='router'][callee.property.name='navigate']",
          message:
            'Direct router.navigate from application code breaks the Coordinator protocol (MUL-4741). Route it through the navigation adapter in src/platform.',
        },
      ],
    },
  },
];

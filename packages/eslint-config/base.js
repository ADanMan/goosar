import eslint from '@eslint/js';
import tseslint from 'typescript-eslint';
import importPlugin from 'eslint-plugin-import-x';

export default [
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  {
    plugins: {
      'import-x': importPlugin,
    },
    rules: {
      '@typescript-eslint/no-unused-vars': 'off',
      '@typescript-eslint/no-explicit-any': 'off',
      'import-x/no-extraneous-dependencies': [
        'error',
        {
          devDependencies: [
            '**/*.test.{ts,tsx}',
            '**/*.spec.{ts,tsx}',
            '**/test/**',
            '**/tests/**',
            '**/vitest.config.*',
            '**/vite.config.*',
            '**/electron.vite.config.*',
            '**/eslint.config.*',
            '**/scripts/**',
            '**/src/main/**',
            '**/src/preload/**',
          ],
          peerDependencies: true,
        },
      ],
    },
  },
  {
    ignores: ['node_modules/', 'dist/', '.next/', 'out/'],
  },
];

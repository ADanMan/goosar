// Shared hex palette for user-picked label/property colours. These are data
// (the palette IS the thing being chosen), not theme colours, so they stay
// as literal hex values rather than design tokens — see the allow-list in
// scripts/check-ui-tokens.mjs.
export const LABEL_COLORS = [
  '#6b7280',
  '#ef4444',
  '#f97316',
  '#eab308',
  '#22c55e',
  '#14b8a6',
  '#3b82f6',
  '#6366f1',
  '#a855f7',
  '#ec4899',
] as const;

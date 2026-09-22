import { common, createLowlight } from 'lowlight';

const baseLowlight = createLowlight(common);

export const codeLowlight: ReturnType<typeof createLowlight> = {
  ...baseLowlight,
  highlightAuto(value) {
    return baseLowlight.highlight('plaintext', value);
  },
};

export function highlightCode(value: string, language?: string) {
  const normalizedLanguage = language?.trim().toLowerCase();

  return normalizedLanguage && baseLowlight.registered(normalizedLanguage)
    ? baseLowlight.highlight(normalizedLanguage, value)
    : baseLowlight.highlight('plaintext', value);
}

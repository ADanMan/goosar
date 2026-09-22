import { defaultUrlTransform } from 'react-markdown';
import { defaultSchema, type Options } from 'rehype-sanitize';
import { isTrustedMarkdownImageSrc } from './image-policy';

export const markdownSanitizeSchema: Options = {
  ...defaultSchema,
  tagNames: [...(defaultSchema.tagNames ?? []), 'mark'],
  protocols: {
    ...defaultSchema.protocols,
    href: [...(defaultSchema.protocols?.href ?? []), 'mention', 'slash'],
    src: [...(defaultSchema.protocols?.src ?? []), 'data'],
  },
  attributes: {
    ...defaultSchema.attributes,
    div: [...(defaultSchema.attributes?.div ?? []), 'dataType', 'dataHref', 'dataFilename'],
    code: [
      ...(defaultSchema.attributes?.code ?? []),
      ['className', /^language-/],
      ['className', /^math-/],
      ['className', /^hljs/],
    ],
    img: [
      ...(defaultSchema.attributes?.img ?? []).filter(
        (attr) => (typeof attr === 'string' ? attr : attr[0]) !== 'src',
      ),
      'alt',
      ['src', /^data:image\//i, /^(?!data:)/i],
    ],
  },
};

export function markdownUrlTransform(url: string, key?: string): string {
  if (url.startsWith('mention://')) return url;
  if (url.startsWith('slash://skill/')) return url;
  if (/^data:image\//i.test(url)) return url;
  if (key === 'src' && !isTrustedMarkdownImageSrc(url)) return '';
  return defaultUrlTransform(url);
}

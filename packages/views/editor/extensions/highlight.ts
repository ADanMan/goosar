import Highlight from '@tiptap/extension-highlight';
import { matchHighlightAt } from '../utils/highlight-match';

export const HighlightExtension = Highlight.extend({
  markdownTokenizer: {
    name: 'highlight',
    level: 'inline' as const,
    start(src: string) {
      return src.indexOf('==');
    },
    tokenize(src: string, _tokens: unknown, helpers: any) {
      const match = matchHighlightAt(src, 0);
      if (!match) return undefined;
      return {
        type: 'highlight',
        raw: src.slice(0, match.end),
        tokens: helpers.inlineTokens(match.inner),
      };
    },
  },

  parseMarkdown: (token: any, helpers: any) =>
    helpers.applyMark('highlight', helpers.parseInline(token.tokens)),

  renderMarkdown: (_node: any, helpers: any) => `==${helpers.renderChildren()}==`,
}).configure({ multicolor: false });

import {
  preprocessLinks,
  preprocessMentionShortcodes,
  preprocessFileCards,
  preprocessIssueIdentifiers,
} from '@goosar/ui/markdown';

export function preprocessMarkdown(
  markdown: string,
  opts: { cdnDomain: string; autolinkIssueIdentifiers?: boolean },
): string {
  if (!markdown) return '';
  const { cdnDomain } = opts;
  const step1 = preprocessMentionShortcodes(markdown);
  const step2 = opts?.autolinkIssueIdentifiers ? preprocessIssueIdentifiers(step1) : step1;
  const step3 = preprocessLinks(step2);
  const step4 = preprocessFileCards(step3, cdnDomain);
  return step4;
}

// Удаление markdown-синтаксиса упоминаний до простого текста.
export function stripMentionMarkdown(text: string): string {
  return text.replace(
    /(?<![\\])\[(@?)((?:\\.|[^\]])+)\]\(mention:\/\/\w+\/[^)]+\)/g,
    (_, prefix: string, rawLabel: string) => {
      const label = rawLabel.replace(/\\\[/g, '[').replace(/\\\]/g, ']');
      return `${prefix}${label}`;
    },
  );
}

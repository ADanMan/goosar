import { CheckCircle2, ChevronRight } from 'lucide-react';
import { useActorName } from '@goosar/core/workspace/hooks';
import { Card } from '@goosar/ui/components/ui/card';
import type { TimelineEntry } from '@goosar/core/types';
import { useT } from '../../i18n';

interface ResolvedThreadBarProps {
  entry: TimelineEntry;
  replies: TimelineEntry[];
  onExpand: () => void;
}

const MAX_NAMED_AUTHORS = 2;

function useAuthorsLabel(entries: TimelineEntry[]): string {
  const { t } = useT('issues');
  const { getActorName } = useActorName();

  const seen = new Set<string>();
  const authors: Array<{ type: string; id: string }> = [];
  for (const e of entries) {
    const key = `${e.actor_type}:${e.actor_id}`;
    if (seen.has(key)) continue;
    seen.add(key);
    authors.push({ type: e.actor_type, id: e.actor_id });
  }

  if (authors.length <= MAX_NAMED_AUTHORS) {
    return authors.map((a) => getActorName(a.type, a.id)).join(', ');
  }
  const named = authors
    .slice(0, MAX_NAMED_AUTHORS)
    .map((a) => getActorName(a.type, a.id))
    .join(', ');
  return t(($) => $.comment.resolve.bar_authors_more, {
    names: named,
    count: authors.length - MAX_NAMED_AUTHORS,
  });
}

export function ResolvedThreadBar({ entry, replies, onExpand }: ResolvedThreadBarProps) {
  const { t } = useT('issues');
  const authorsLabel = useAuthorsLabel([entry, ...replies]);
  const count = 1 + replies.length;

  return (
    <Card className="!py-0 !gap-0 overflow-hidden">
      <button
        type="button"
        onClick={onExpand}
        className="flex w-full items-center justify-between px-4 py-3 text-left transition-colors cursor-pointer hover:bg-muted/50"
      >
        <span className="flex min-w-0 items-center gap-2.5 text-sm text-muted-foreground">
          <CheckCircle2 className="h-4 w-4 shrink-0" />
          <span className="truncate">
            {t(($) => $.comment.resolve.bar, { count, authors: authorsLabel })}
          </span>
        </span>
        <ChevronRight className="h-3.5 w-3.5 rotate-90 shrink-0 text-muted-foreground" />
      </button>
    </Card>
  );
}

interface CommentsFoldBarProps {
  replies: TimelineEntry[];
  onExpand: () => void;
}

export function CommentsFoldBar({ replies, onExpand }: CommentsFoldBarProps) {
  const { t } = useT('issues');
  const authorsLabel = useAuthorsLabel(replies);

  return (
    <button
      type="button"
      onClick={onExpand}
      className="flex w-full items-center justify-between rounded-md bg-muted/45 px-3 py-2.5 text-left transition-colors cursor-pointer hover:bg-muted"
    >
      <span className="flex min-w-0 items-center gap-2.5 text-sm text-muted-foreground">
        <ChevronRight className="h-3.5 w-3.5 rotate-90 shrink-0" />
        <span className="truncate">
          {t(($) => $.comment.resolve.fold, { count: replies.length, authors: authorsLabel })}
        </span>
      </span>
    </button>
  );
}

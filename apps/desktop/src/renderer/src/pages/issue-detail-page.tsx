import { useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { IssueDetail } from '@goosar/views/issues/components';
import { useWorkspaceId } from '@goosar/core/hooks';
import { issueDetailOptions } from '@goosar/core/issues/queries';
import { useDocumentTitle } from '@/hooks/use-document-title';

export function IssueDetailPage({ onDelete }: { onDelete?: () => void }) {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: issue } = useQuery(issueDetailOptions(wsId, id!));

  useDocumentTitle(issue ? `${issue.identifier}: ${issue.title}` : 'Issue');

  if (!id) return null;
  return <IssueDetail issueId={id} onDelete={onDelete} />;
}

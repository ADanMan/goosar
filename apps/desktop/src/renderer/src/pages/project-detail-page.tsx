import { useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ProjectDetail } from '@goosar/views/projects/components';
import { useWorkspaceId } from '@goosar/core/hooks';
import { projectDetailOptions } from '@goosar/core/projects/queries';
import { useDocumentTitle } from '@/hooks/use-document-title';

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: project } = useQuery(projectDetailOptions(wsId, id!));

  useDocumentTitle(project ? project.title : 'Project');

  if (!id) return null;
  return <ProjectDetail projectId={id} />;
}

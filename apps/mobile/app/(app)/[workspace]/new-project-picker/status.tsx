// Выбор статуса для черновика нового проекта. Читает и пишет useNewProjectDraftStore,
// которым владеет модалка project/new.tsx.
import { router } from 'expo-router';
import { ProjectStatusPickerBody } from '@/components/project/pickers/project-status-picker-body';
import { useNewProjectDraftStore } from '@/data/stores/new-project-draft-store';

export default function NewProjectStatusPickerRoute() {
  const status = useNewProjectDraftStore((s) => s.status);
  const setStatus = useNewProjectDraftStore((s) => s.setStatus);

  return (
    <ProjectStatusPickerBody
      value={status}
      onChange={(next) => {
        setStatus(next);
        router.back();
      }}
    />
  );
}

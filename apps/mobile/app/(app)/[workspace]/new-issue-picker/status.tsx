// Выбор статуса для черновика новой задачи. Читает и пишет useNewIssueDraftStore,
// которым владеет модалка new-issue.tsx.
import { router } from 'expo-router';
import { StatusPickerBody } from '@/components/issue/pickers/status-picker-body';
import { useNewIssueDraftStore } from '@/data/stores/new-issue-draft-store';

export default function NewIssueStatusPickerRoute() {
  const status = useNewIssueDraftStore((s) => s.status);
  const setStatus = useNewIssueDraftStore((s) => s.setStatus);

  return (
    <StatusPickerBody
      value={status}
      onChange={(next) => {
        setStatus(next);
        router.back();
      }}
    />
  );
}

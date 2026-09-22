// Выбор меток для существующей задачи — мультивыбор со встроенным созданием,
// нативный заголовок и поиск.
import { useRef } from 'react';
import { useLocalSearchParams } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import { LabelPickerBody } from '@/components/issue/pickers/label-picker-body';
import { issueDetailOptions } from '@/data/queries/issues';
import { useAttachLabel, useDetachLabel } from '@/data/mutations/issues';
import { useCreateLabel } from '@/data/mutations/labels';
import { useWorkspaceStore } from '@/data/workspace-store';
import { useNativeSearchBar } from '@/lib/use-native-search-bar';

export default function IssueLabelPickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, id));
  const attachLabel = useAttachLabel(id);
  const detachLabel = useDetachLabel(id);
  const createLabel = useCreateLabel();
  const query = useNativeSearchBar('Search labels', { autoFocus: true });

  const creatingRef = useRef(false);

  const attached = issue?.labels ?? [];

  return (
    <LabelPickerBody
      attached={attached}
      query={query}
      onAttach={(label) => attachLabel.mutate({ label })}
      onDetach={(labelId) => detachLabel.mutate({ labelId })}
      onCreate={(name, color) => {
        if (creatingRef.current) return;
        creatingRef.current = true;
        createLabel.mutate(
          { name, color },
          {
            onSuccess: (label) => {
              attachLabel.mutate({ label });
            },
            onSettled: () => {
              creatingRef.current = false;
            },
          },
        );
      }}
    />
  );
}

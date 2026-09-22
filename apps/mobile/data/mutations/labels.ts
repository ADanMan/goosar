/**
 * Мутации меток на мобильном клиенте — используют собственный ApiClient
 * и стор рабочего пространства, так как общий хук `useWorkspaceId` мобильному
 * приложению недоступен.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { CreateLabelRequest, Label } from "@goosar/core/types";
import { api } from "@/data/api";
import { labelKeys } from "@/data/queries/labels";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useCreateLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (body: CreateLabelRequest) => api.createLabel(body),
    onSuccess: (label) => {
      qc.setQueryData<Label[]>(labelKeys.all(wsId), (old) =>
        old && !old.some((l) => l.id === label.id) ? [...old, label] : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
    },
  });
}

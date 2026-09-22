// Мутации закреплений: оптимистичные в три шага (снимок → патч → инвалидация),
// зеркалят packages/core/pins/mutations.ts.
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { PinnedItem, PinnedItemType } from "@goosar/core/types";
import { api } from "@/data/api";
import { pinKeys } from "@/data/queries/pins";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useCreatePin() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationFn: (data: { item_type: PinnedItemType; item_id: string }) =>
      api.createPin(data),
    onMutate: async (data) => {
      if (!wsId || !userId) return;
      const key = pinKeys.list(wsId, userId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<PinnedItem[]>(key);
      const stub: PinnedItem = {
        id: `optimistic-${data.item_type}-${data.item_id}`,
        workspace_id: wsId,
        user_id: userId,
        item_type: data.item_type,
        item_id: data.item_id,
        position: (prev?.length ?? 0) + 1,
        created_at: new Date().toISOString(),
      };
      qc.setQueryData<PinnedItem[]>(key, (old) =>
        old ? [...old, stub] : [stub],
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.key && ctx.prev !== undefined) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (newPin) => {
      if (!wsId || !userId) return;
      const key = pinKeys.list(wsId, userId);
      qc.setQueryData<PinnedItem[]>(key, (old) =>
        old
          ? old.map((p) =>
              p.id ===
              `optimistic-${newPin.item_type}-${newPin.item_id}`
                ? newPin
                : p,
            )
          : [newPin],
      );
    },
    onSettled: () => {
      if (!wsId || !userId) return;
      qc.invalidateQueries({ queryKey: pinKeys.list(wsId, userId) });
    },
  });
}

export function useDeletePin() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationFn: ({
      itemType,
      itemId,
    }: {
      itemType: PinnedItemType;
      itemId: string;
    }) => api.deletePin(itemType, itemId),
    onMutate: async ({ itemType, itemId }) => {
      if (!wsId || !userId) return;
      const key = pinKeys.list(wsId, userId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<PinnedItem[]>(key);
      qc.setQueryData<PinnedItem[]>(key, (old) =>
        old
          ? old.filter(
              (p) => !(p.item_type === itemType && p.item_id === itemId),
            )
          : old,
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.key && ctx.prev !== undefined) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      if (!wsId || !userId) return;
      qc.invalidateQueries({ queryKey: pinKeys.list(wsId, userId) });
    },
  });
}

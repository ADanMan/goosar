'use client';

import { create } from 'zustand';
import { createJSONStorage, persist, type StateStorage } from 'zustand/middleware';
import { defaultStorage } from '../platform/storage';

export interface CustomModelPricing {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface CustomPricingState {
  pricings: Record<string, CustomModelPricing>;
  setCustomPricing: (model: string, pricing: CustomModelPricing) => void;
  removeCustomPricing: (model: string) => void;
}

const stateStorage = defaultStorage as unknown as StateStorage;

export const useCustomPricingStore = create<CustomPricingState>()(
  persist(
    (set) => ({
      pricings: {},
      setCustomPricing: (model, pricing) =>
        set((state) => ({
          pricings: { ...state.pricings, [model]: pricing },
        })),
      removeCustomPricing: (model) =>
        set((state) => {
          if (!(model in state.pricings)) return state;
          const next = { ...state.pricings };
          delete next[model];
          return { pricings: next };
        }),
    }),
    {
      name: 'goosar_runtime_custom_pricing',
      storage: createJSONStorage(() => stateStorage),
    },
  ),
);

export function getCustomPricing(model: string): CustomModelPricing | undefined {
  return useCustomPricingStore.getState().pricings[model];
}

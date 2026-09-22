export const UPDATE_CONTROL_CHANNEL = 'updater:delivery-control';

export interface UpdateControl {
  perimeterProfile: boolean;
}

export const UPDATE_CONTROL_CLOUD: UpdateControl = {
  perimeterProfile: false,
};

export function parseUpdateControl(value: unknown): UpdateControl {
  if (!value || typeof value !== 'object') return UPDATE_CONTROL_CLOUD;
  const input = value as Record<string, unknown>;
  return { perimeterProfile: input.perimeterProfile === true };
}

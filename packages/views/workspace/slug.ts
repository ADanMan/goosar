export const WORKSPACE_SLUG_REGEX = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

export function nameToWorkspaceSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

export function isWorkspaceSlugConflict(error: unknown): boolean {
  return (
    typeof error === 'object' &&
    error !== null &&
    'status' in error &&
    (error as { status?: unknown }).status === 409
  );
}

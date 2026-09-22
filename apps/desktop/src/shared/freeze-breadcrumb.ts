// Метка о зависании/падении, сохраняемая главным процессом и отправляемая
// в телеметрию при следующем старте рендерера. Общая для main, preload и renderer.
export interface FreezeBreadcrumb {
  ownerId?: string;
  kind: string;
  context: Record<string, unknown>;
  ts: number;
  version: string;
}

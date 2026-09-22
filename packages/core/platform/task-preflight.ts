// Хук платформы для остановки задачи/чата до отправки: общий интерфейс чата
// не знает, что конкретной машине сначала нужен билет Kerberos.

export type TaskPreflight = () => Promise<boolean>;

let preflight: TaskPreflight | null = null;

export function setTaskPreflight(check: TaskPreflight | null): void {
  preflight = check;
}

export async function runTaskPreflight(): Promise<boolean> {
  if (preflight === null) return true;
  try {
    return await preflight();
  } catch {
    return true;
  }
}

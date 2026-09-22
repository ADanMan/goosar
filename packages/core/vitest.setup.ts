/**
 * Восстанавливает рабочие глобальные Web Storage для тестов jsdom под
 * Node >= 22.4. Современный Node подменяет `globalThis.localStorage` /
 * `sessionStorage` экспериментальными геттерами, возвращающими `undefined`
 * без флага `--localstorage-file`, и vitest не перекрывает уже существующий
 * глобал рабочей реализацией jsdom — отсюда падения `localStorage.*`.
 * Настоящий Storage jsdom здесь недоступен (worker переопределяет `window`
 * на глобал), поэтому вместо него ставится in-memory реализация с тем же
 * наблюдаемым поведением: состояние живёт в пределах одного файла тестов.
 * Правится только тестовое окружение, продакшен-код не затрагивается.
 */

function createMemoryStorage(): Storage {
  const data = new Map<string, string>();
  return {
    get length(): number {
      return data.size;
    },
    clear(): void {
      data.clear();
    },
    getItem(key: string): string | null {
      return data.get(String(key)) ?? null;
    },
    key(index: number): string | null {
      return Array.from(data.keys())[index] ?? null;
    },
    removeItem(key: string): void {
      data.delete(String(key));
    },
    setItem(key: string, value: string): void {
      data.set(String(key), String(value));
    },
  };
}

const isBrowserLikeEnvironment = typeof window !== 'undefined';

if (isBrowserLikeEnvironment) {
  for (const key of ['localStorage', 'sessionStorage'] as const) {
    if ((globalThis as Record<string, unknown>)[key] !== undefined) continue;
    Object.defineProperty(globalThis, key, {
      value: createMemoryStorage(),
      configurable: true,
      writable: true,
      enumerable: true,
    });
  }
}

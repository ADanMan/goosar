// Общая заглушка @goosar/views/i18n для тестов рендерера.

export async function settingsI18nMock() {
  const { RESOURCES } = await import('@goosar/views/locales');
  const en = RESOURCES.en.settings as Record<string, unknown>;

  const translate = (
    accessor: (dict: unknown) => unknown,
    params?: Record<string, unknown>,
  ): string => {
    let template = accessor(en);

    if (typeof template !== 'string') {
      const path: string[] = [];
      const recorder: unknown = new Proxy(
        {},
        { get: (_target, prop) => (path.push(String(prop)), recorder) },
      );
      accessor(recorder);

      let node: unknown = en;
      for (const segment of path.slice(0, -1)) {
        node = (node as Record<string, unknown> | undefined)?.[segment];
      }
      const leaf = path[path.length - 1];
      const suffix = params?.count === 1 ? 'one' : 'other';
      template = (node as Record<string, unknown> | undefined)?.[`${leaf}_${suffix}`];
    }

    if (typeof template !== 'string') return '';
    return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) => String(params?.[key] ?? ''));
  };

  return { useT: () => ({ t: translate }) };
}

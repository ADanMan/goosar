// Формирует markdown-цитату с описанием пользователя из анкеты онбординга
// для стартовых задач Helper.

export interface UserContextLabels {
  heading: string;
  roleLabel: string;
  useCaseLabel: string;
  listSeparator: string;
  role: Record<string, string>;
  useCase: Record<string, string>;
}

export interface QuestionnaireRaw {
  role?: unknown;
  role_other?: unknown;
  use_case?: unknown;
  use_case_other?: unknown;
}

function asStringArray(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter((v): v is string => typeof v === 'string' && v.length > 0);
  }
  if (typeof value === 'string' && value.length > 0) return [value];
  return [];
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

export function buildUserContextSection(
  raw: QuestionnaireRaw | undefined | null,
  labels: UserContextLabels,
): string {
  if (!raw) return '';

  const role = asString(raw.role);
  const roleOther = asString(raw.role_other);
  const roleDisplay = role === 'other' ? roleOther : role ? (labels.role[role] ?? role) : '';

  const useCaseSlugs = asStringArray(raw.use_case);
  const useCaseOther = asString(raw.use_case_other);
  const useCaseDisplays = useCaseSlugs
    .map((slug) => (slug === 'other' ? useCaseOther : (labels.useCase[slug] ?? slug)))
    .filter((s) => s.length > 0);

  const hasRole = roleDisplay.length > 0;
  const hasUseCase = useCaseDisplays.length > 0;
  if (!hasRole && !hasUseCase) return '';

  const lines: string[] = ['', '', '---', '', `> **${labels.heading}**`, '>'];
  if (hasRole) lines.push(`> ${labels.roleLabel}: ${roleDisplay}`);
  if (hasUseCase) {
    lines.push(`> ${labels.useCaseLabel}: ${useCaseDisplays.join(labels.listSeparator)}`);
  }
  return lines.join('\n');
}

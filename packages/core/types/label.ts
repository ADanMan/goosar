/**
 * Метки issue — область видимости workspace, связь многие-ко-многим с issue.
 *
 * Метка — лёгкая метаданная (имя + цвет), в отличие от проекта: проект
 * группирует связанную работу, метка — сквозной тег (баг, фича,
 * производительность…). Цвет нормализуется к нижнему регистру `#RRGGBB`.
 */
export type LabelResourceType = 'issue' | 'agent' | 'skill';

export interface Label {
  id: string;
  workspace_id: string;
  resource_type?: LabelResourceType;
  name: string;
  description?: string;
  color: string;
  usage_count?: number;
  created_at: string;
  updated_at: string;
}

export interface CreateLabelRequest {
  resource_type?: LabelResourceType;
  name: string;
  description?: string;
  color: string;
}

export interface UpdateLabelRequest {
  name?: string;
  description?: string;
  color?: string;
}

export interface ListLabelsResponse {
  labels: Label[];
  total: number;
}

export interface IssueLabelsResponse {
  labels: Label[];
}

export type ResourceLabelsResponse = IssueLabelsResponse;

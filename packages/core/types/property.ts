/**
 * Кастомные свойства issue — типизированные поля, заданные на уровне
 * workspace. Определения хранятся в каталоге workspace (доступен только
 * owner/admin); значения лежат на каждом issue в наборе, ключом к которому
 * служит id определения, поэтому переименование не трогает строки issue.
 *
 * Значение типизировано по определению: select хранит id опции,
 * multi_select — массив id опций (в порядке конфига), date — строку
 * "YYYY-MM-DD", checkbox — булево, number — число, text/url — строки.
 */
export type IssuePropertyType =
  'text' | 'number' | 'select' | 'multi_select' | 'date' | 'checkbox' | 'url';

export const ISSUE_PROPERTY_TYPES: IssuePropertyType[] = [
  'text',
  'number',
  'select',
  'multi_select',
  'date',
  'checkbox',
  'url',
];

export function isKnownPropertyType(type: string): type is IssuePropertyType {
  return (ISSUE_PROPERTY_TYPES as string[]).includes(type);
}

export interface IssuePropertyOption {
  id: string;
  name: string;
  color: string;
}

export interface IssuePropertyConfig {
  options?: IssuePropertyOption[];
}

export interface IssueProperty {
  id: string;
  workspace_id: string;
  name: string;
  type: string;
  description?: string;
  icon?: string;
  config: IssuePropertyConfig;
  position: number;
  archived: boolean;
  archived_at?: string | null;
  usage_count?: number;
  created_at: string;
  updated_at: string;
}

export type IssuePropertyValue = string | number | boolean | string[];
export type IssuePropertyValues = Record<string, IssuePropertyValue>;

export interface CreatePropertyRequest {
  name: string;
  type: IssuePropertyType;
  description?: string;
  icon?: string;
  config?: IssuePropertyConfig;
}

export interface UpdatePropertyRequest {
  name?: string;
  description?: string;
  icon?: string;
  config?: IssuePropertyConfig;
  archived?: boolean;
}

export interface ListPropertiesResponse {
  properties: IssueProperty[];
  total: number;
}

export interface IssuePropertiesResponse {
  properties: IssuePropertyValues;
}

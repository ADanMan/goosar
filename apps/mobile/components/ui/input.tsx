/**
 * Шим для обратной совместимости: реэкспортирует TextField как Input,
 * чтобы старый импорт продолжал работать. Новый код должен использовать
 * TextField или AutosizeTextArea напрямую.
 */

export { TextField as Input } from './text-field';
export type { TextFieldProps as InputProps } from './text-field';

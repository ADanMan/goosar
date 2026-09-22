/**
 * Мобильный Collapsible — обёртка над @rn-primitives/collapsible в
 * стиле shadcn: Root + Trigger + Content, аналог web-версии. Обёртка
 * тонкая — классы по умолчанию не навязываются, это решает вызывающий
 * код. Анимация высоты при открытии не добавлена.
 */

import * as CollapsiblePrimitive from '@rn-primitives/collapsible';

const Collapsible = CollapsiblePrimitive.Root;
const CollapsibleTrigger = CollapsiblePrimitive.Trigger;
const CollapsibleContent = CollapsiblePrimitive.Content;

export { Collapsible, CollapsibleContent, CollapsibleTrigger };

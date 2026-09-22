import type { Attachment } from '@goosar/core/types';
import { contentReferencesAttachment } from '@goosar/core/types';

export function standaloneAttachments(
  attachments: Attachment[] | undefined,
  content: string | undefined,
): Attachment[] {
  if (!attachments || attachments.length === 0) return [];
  if (!content) return attachments;
  return attachments.filter((a) => {
    if (contentReferencesAttachment(content, a)) return false;
    const hasSiblingInContent = attachments.some(
      (other) =>
        other.id !== a.id &&
        other.filename === a.filename &&
        other.content_type === a.content_type &&
        other.size_bytes === a.size_bytes &&
        contentReferencesAttachment(content, other),
    );
    return !hasSiblingInContent;
  });
}

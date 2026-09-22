'use client';

import { memo } from 'react';
import type { Attachment } from '@goosar/core/types';
import { RichContent } from '../rich-content';

interface ReadonlyContentProps {
  content: string;
  className?: string;
  attachments?: Attachment[];
}

export const ReadonlyContent = memo(function ReadonlyContent({
  content,
  className,
  attachments,
}: ReadonlyContentProps) {
  return (
    <RichContent
      content={content}
      attachments={attachments}
      density="document"
      phase="settled"
      className={className}
    />
  );
});

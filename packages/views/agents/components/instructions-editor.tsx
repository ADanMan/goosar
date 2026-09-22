'use client';

import { useRef, useState } from 'react';
import { ChevronDown, FileText, X } from 'lucide-react';
import { ContentEditor, type ContentEditorRef } from '../../editor';
import { Button } from '@goosar/ui/components/ui/button';
import { cn } from '@goosar/ui/lib/utils';
import { useT } from '../../i18n';

interface InstructionsEditorProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}

export function InstructionsEditor({ value, onChange, placeholder }: InstructionsEditorProps) {
  const { t } = useT('agents');
  const [expanded, setExpanded] = useState(false);
  const editorRef = useRef<ContentEditorRef>(null);

  const label = t(($) => $.create_dialog.instructions.label);
  const resolvedPlaceholder =
    placeholder ?? t(($) => $.create_dialog.instructions.placeholder_blank);

  const expand = () => {
    setExpanded(true);
    setTimeout(() => editorRef.current?.focus(), 0);
  };

  if (!expanded) {
    return (
      <div>
        <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </div>
        <button
          type="button"
          onClick={expand}
          className="mt-1.5 flex w-full items-start gap-2.5 rounded-lg border bg-card px-3 py-3 text-left transition-colors hover:border-primary/40 hover:bg-accent/40"
        >
          <FileText className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            {value.trim() ? (
              <div className="line-clamp-2 whitespace-pre-wrap text-sm text-foreground/80">
                {value}
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">{resolvedPlaceholder}</div>
            )}
          </div>
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground/40" />
        </button>
      </div>
    );
  }

  return (
    <div>
      <div className="flex items-center justify-between">
        <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setExpanded(false)}
          className="h-6 gap-1 px-2 text-xs"
        >
          <X className="h-3 w-3" />
          {t(($) => $.create_dialog.instructions.collapse)}
        </Button>
      </div>
      <div className={cn('mt-1.5 rounded-lg border bg-card', 'focus-within:border-primary/40')}>
        <ContentEditor
          ref={editorRef}
          defaultValue={value}
          onUpdate={onChange}
          placeholder={t(($) => $.create_dialog.instructions.editor_placeholder)}
          className="min-h-[160px] max-h-[320px] overflow-y-auto px-3 py-2.5 text-sm"
          showBubbleMenu={true}
          disableMentions={true}
        />
      </div>
    </div>
  );
}

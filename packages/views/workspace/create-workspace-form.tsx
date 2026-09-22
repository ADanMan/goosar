'use client';

import { useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { Button } from '@goosar/ui/components/ui/button';
import { Card, CardContent } from '@goosar/ui/components/ui/card';
import { RadioGroup, RadioGroupItem } from '@goosar/ui/components/ui/radio-group';
import { useCreateWorkspace } from '@goosar/core/workspace/mutations';
import { workspaceTemplateListOptions } from '@goosar/core/workspace/queries';
import type { Workspace } from '@goosar/core/types';
import { isImeComposing } from '@goosar/core/utils';
import { WORKSPACE_SLUG_REGEX, isWorkspaceSlugConflict, nameToWorkspaceSlug } from './slug';
import { useT } from '../i18n';
import { describeServerFailure } from '../common/server-error';
import { isReservedSlug } from '@goosar/core/paths';
import { useConfigStore } from '@goosar/core/config';
import { workspaceUrlHost } from '@goosar/core/workspace/workspace-url';

export interface CreateWorkspaceFormProps {
  onSuccess: (workspace: Workspace) => void | Promise<void>;
}

export function CreateWorkspaceForm({ onSuccess }: CreateWorkspaceFormProps) {
  const { t } = useT('workspace');
  const { t: tCommon } = useT('common');
  const createWorkspace = useCreateWorkspace();
  const urlHost = workspaceUrlHost(useConfigStore((s) => s.daemonAppUrl));
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [slugServerError, setSlugServerError] = useState<string | null>(null);
  const slugTouched = useRef(false);
  const [templateKey, setTemplateKey] = useState('');
  const templates = useQuery(workspaceTemplateListOptions()).data ?? [];

  const slugValidationError =
    slug.length > 0 && !WORKSPACE_SLUG_REGEX.test(slug)
      ? t(($) => $.create_form.errors.slug_format)
      : null;
  const slugReservedError =
    slug.length > 0 && isReservedSlug(slug) ? t(($) => $.create_form.errors.slug_reserved) : null;
  const slugError = slugValidationError ?? slugReservedError ?? slugServerError;
  const canSubmit = name.trim().length > 0 && slug.trim().length > 0 && !slugError;

  const handleNameChange = (value: string) => {
    setName(value);
    if (!slugTouched.current) {
      setSlug(nameToWorkspaceSlug(value));
      setSlugServerError(null);
    }
  };

  const handleSlugChange = (value: string) => {
    slugTouched.current = true;
    setSlug(value);
    setSlugServerError(null);
  };

  const handleCreate = () => {
    if (!canSubmit) return;
    createWorkspace.mutate(
      {
        name: name.trim(),
        slug: slug.trim(),
        ...(templateKey !== '' ? { template_key: templateKey } : {}),
      },
      {
        onSuccess,
        onError: (error) => {
          if (isWorkspaceSlugConflict(error)) {
            setSlugServerError(t(($) => $.create_form.errors.slug_taken));
            toast.error(t(($) => $.create_form.errors.slug_conflict_toast));
            return;
          }
          const failure = describeServerFailure(
            tCommon,
            error,
            t(($) => $.create_form.errors.create_failed),
          );
          toast.error(
            failure.text,
            failure.detail === undefined ? undefined : { description: failure.detail },
          );
        },
      },
    );
  };

  return (
    <Card className="w-full">
      <CardContent className="space-y-4 pt-6">
        <div className="space-y-1.5">
          <Label htmlFor="ws-name">{t(($) => $.create_form.name_label)}</Label>
          <Input
            id="ws-name"
            autoFocus
            type="text"
            value={name}
            onChange={(e) => handleNameChange(e.target.value)}
            placeholder={t(($) => $.create_form.name_placeholder)}
            onKeyDown={(e) => {
              if (isImeComposing(e)) return;
              if (e.key === 'Enter') handleCreate();
            }}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="ws-slug">{t(($) => $.create_form.url_label)}</Label>
          <div className="flex items-center gap-0 rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring">
            <span className="pl-3 text-sm text-muted-foreground select-none">{`${urlHost}/`}</span>
            <Input
              id="ws-slug"
              type="text"
              value={slug}
              onChange={(e) => handleSlugChange(e.target.value)}
              placeholder={t(($) => $.create_form.url_placeholder)}
              className="border-0 shadow-none focus-visible:ring-0"
              onKeyDown={(e) => {
                if (isImeComposing(e)) return;
                if (e.key === 'Enter') handleCreate();
              }}
            />
          </div>
          {slugError && <p className="text-xs text-destructive">{slugError}</p>}
        </div>
        {templates.length > 0 && (
          <div className="space-y-1.5">
            <Label>{t(($) => $.create_form.template_label)}</Label>
            <RadioGroup
              value={templateKey}
              onValueChange={(value) => setTemplateKey(typeof value === 'string' ? value : '')}
              className="gap-1.5"
            >
              {/* "No template" is always first and the default: the
                  untemplated path is the product's baseline behavior.
                  Base UI's Radio renders a button, so a wrapping label
                  does not forward clicks — the row onClick makes the
                  whole card clickable; both paths set the same state. */}
              <label
                className="flex cursor-pointer items-start gap-3 rounded-md border p-3"
                onClick={() => setTemplateKey('')}
              >
                <RadioGroupItem value="" className="mt-0.5" />
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="text-sm font-medium">
                    {t(($) => $.create_form.template_none)}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {t(($) => $.create_form.template_none_description)}
                  </span>
                </span>
              </label>
              {templates.map((tmpl) => (
                <label
                  key={tmpl.key}
                  className="flex cursor-pointer items-start gap-3 rounded-md border p-3"
                  onClick={() => setTemplateKey(tmpl.key)}
                >
                  <RadioGroupItem value={tmpl.key} className="mt-0.5" />
                  <span className="flex min-w-0 flex-col gap-0.5">
                    <span className="text-sm font-medium break-words">{tmpl.name}</span>
                    {tmpl.description !== '' && (
                      <span className="text-xs text-muted-foreground break-words">
                        {tmpl.description}
                      </span>
                    )}
                  </span>
                </label>
              ))}
            </RadioGroup>
          </div>
        )}
        <Button
          className="w-full"
          size="lg"
          onClick={handleCreate}
          disabled={createWorkspace.isPending || !canSubmit}
        >
          {createWorkspace.isPending
            ? t(($) => $.create_form.submitting)
            : t(($) => $.create_form.submit)}
        </Button>
      </CardContent>
    </Card>
  );
}

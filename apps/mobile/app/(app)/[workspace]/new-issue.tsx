// Модалка создания задачи: одна вертикальная форма (название → описание → чипы
// свойств) по образцу Apple Reminders / Linear / Things.
import { useCallback, useEffect, useState } from 'react';
import { Alert, KeyboardAvoidingView, Platform, ScrollView, TextInput } from 'react-native';
import { Stack, router } from 'expo-router';
import { SubmitIssueButton } from '@/components/issue/submit-issue-button';
import { CreateFormAttributeRow } from '@/components/issue/create-form-attribute-row';
import { MentionSuggestionBar } from '@/components/issue/mention-suggestion-bar';
import { DescriptionField } from '@/components/issue/description-field';
import { MOBILE_PLACEHOLDER_COLOR } from '@/components/ui/input-tokens';
import { useCreateIssue } from '@/data/mutations/issues';
import { useNewIssueDraftStore } from '@/data/stores/new-issue-draft-store';
import { useMentionInput } from '@/lib/use-mention-input';

export default function NewIssueModal() {
  const [title, setTitle] = useState('');
  const description = useMentionInput();
  const status = useNewIssueDraftStore((s) => s.status);
  const priority = useNewIssueDraftStore((s) => s.priority);
  const assignee = useNewIssueDraftStore((s) => s.assignee);
  const dueDate = useNewIssueDraftStore((s) => s.dueDate);
  const project = useNewIssueDraftStore((s) => s.project);
  const resetDraft = useNewIssueDraftStore((s) => s.reset);

  useEffect(() => {
    resetDraft();
    return () => {
      resetDraft();
    };
  }, [resetDraft]);

  const createIssue = useCreateIssue();
  const isSubmitting = createIssue.isPending;

  const canSubmit = !isSubmitting && title.trim().length > 0;

  const onSubmit = useCallback(async () => {
    const trimmedTitle = title.trim();
    if (trimmedTitle.length === 0) return;
    const finalDescription = description.serialize().trim();
    try {
      await createIssue.mutateAsync({
        title: trimmedTitle,
        description: finalDescription || undefined,
        status,
        priority,
        ...(assignee ? { assignee_type: assignee.type, assignee_id: assignee.id } : {}),
        ...(dueDate ? { due_date: dueDate } : {}),
        ...(project ? { project_id: project.id } : {}),
      });
      router.back();
    } catch (err) {
      Alert.alert('Failed to create issue', err instanceof Error ? err.message : 'Unknown error');
    }
  }, [title, description, status, priority, assignee, dueDate, project, createIssue]);

  const headerRight = useCallback(
    () => <SubmitIssueButton disabled={!canSubmit} loading={isSubmitting} onPress={onSubmit} />,
    [canSubmit, isSubmitting, onSubmit],
  );

  return (
    <>
      <Stack.Screen options={{ headerRight }} />
      <KeyboardAvoidingView
        className="flex-1 bg-background"
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <ScrollView
          className="flex-1"
          contentContainerClassName="px-4 pt-4 pb-6 gap-4"
          keyboardShouldPersistTaps="handled"
        >
          <TextInput
            value={title}
            onChangeText={setTitle}
            placeholder="Issue title"
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            className="text-2xl font-semibold text-foreground py-2"
            autoFocus
            returnKeyType="next"
            editable={!isSubmitting}
          />
          <DescriptionField description={description} disabled={isSubmitting} />
          <CreateFormAttributeRow />
        </ScrollView>

        {/* Mention suggestions float above the keyboard only when the user
            types `@`. Self-hides via `if (!visible) return null` so it
            doesn't take space at rest. */}
        <MentionSuggestionBar {...description.suggestionBar} />
      </KeyboardAvoidingView>
    </>
  );
}

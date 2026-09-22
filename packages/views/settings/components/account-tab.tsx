'use client';

import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { RotateCcw } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Textarea } from '@goosar/ui/components/ui/textarea';
import { toast } from 'sonner';
import { useAuthStore } from '@goosar/core/auth';
import { api } from '@goosar/core/api';
import { paths } from '@goosar/core/paths';
import { AvatarUploadControl } from '../../common/avatar-upload-control';
import { useNavigation } from '../../navigation';
import { useT } from '../../i18n';
import {
  SettingsCard,
  SettingsRow,
  SettingsSaveState,
  SettingsSection,
  SettingsTab,
} from './settings-layout';
import { useAutoSave } from './use-auto-save';

const MAX_PROFILE_DESCRIPTION_LEN = 2000;

interface ProfileDraft {
  name: string;
  profileDescription: string;
}

function profilesEqual(left: ProfileDraft, right: ProfileDraft) {
  return left.name === right.name && left.profileDescription === right.profileDescription;
}

export interface AccountTabProps {
  kerberosSlot?: ReactNode;
  hasKerberos?: boolean;
}

export function AccountTab({ kerberosSlot, hasKerberos }: AccountTabProps = {}) {
  const { t } = useT('settings');
  const navigation = useNavigation();
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);

  const [profileName, setProfileName] = useState(user?.name ?? '');
  const [profileDescription, setProfileDescription] = useState(user?.profile_description ?? '');

  useEffect(() => {
    setProfileName(user?.name ?? '');
    setProfileDescription(user?.profile_description ?? '');
    // Preserve in-progress edits when an avatar upload or auto-save replaces
    // the current user object in the auth store.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentionally keyed on user identity
  }, [user?.id]);

  const descriptionTooLong = profileDescription.length > MAX_PROFILE_DESCRIPTION_LEN;

  const draft = useMemo(
    () => ({ name: profileName, profileDescription }),
    [profileDescription, profileName],
  );
  const savedDraft = useMemo(
    () => ({
      name: user?.name ?? '',
      profileDescription: user?.profile_description ?? '',
    }),
    [user?.name, user?.profile_description],
  );
  const saveProfile = useCallback(
    async (next: ProfileDraft) => {
      const updated = await api.updateMe({
        name: next.name,
        profile_description: next.profileDescription,
      });
      setUser(updated);
    },
    [setUser],
  );
  const autoSave = useAutoSave({
    value: draft,
    savedValue: savedDraft,
    onSave: saveProfile,
    onSuccess: () =>
      toast.success(
        t(($) => $.account.toast_profile_updated),
        {
          id: 'settings-auto-save',
        },
      ),
    onError: (error) =>
      toast.error(
        error instanceof Error ? error.message : t(($) => $.account.toast_profile_failed),
      ),
    enabled: !!user && !!profileName.trim() && !descriptionTooLong,
    isEqual: profilesEqual,
  });

  return (
    <SettingsTab title={t(($) => $.page.tabs.profile)}>
      <SettingsSection
        title={t(($) => $.account.section_profile)}
        action={
          <SettingsSaveState
            status={autoSave.status}
            savingLabel={t(($) => $.auto_save.saving)}
            savedLabel={t(($) => $.auto_save.saved)}
            errorLabel={t(($) => $.auto_save.failed)}
          />
        }
      >
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.account.avatar_label)}
            description={t(($) => $.account.click_avatar_hint)}
            size="none"
          >
            <div className="flex justify-start sm:justify-end">
              <AvatarUploadControl
                variant="user"
                value={user?.avatar_url ?? null}
                name={user?.name ?? ''}
                size={64}
                onUploaded={async (url) => {
                  try {
                    const updated = await api.updateMe({ avatar_url: url });
                    setUser(updated);
                    toast.success(
                      t(($) => $.account.toast_avatar_updated),
                      {
                        id: 'settings-auto-save',
                      },
                    );
                  } catch (error) {
                    toast.error(
                      error instanceof Error
                        ? error.message
                        : t(($) => $.account.toast_avatar_failed),
                    );
                  }
                }}
              />
            </div>
          </SettingsRow>

          <SettingsRow label={t(($) => $.account.name_label)} size="text">
            <Input
              type="text"
              name="profile-name"
              autoComplete="name"
              aria-label={t(($) => $.account.name_label)}
              value={profileName}
              onChange={(event) => setProfileName(event.target.value)}
              onBlur={autoSave.flush}
            />
          </SettingsRow>

          <SettingsRow
            label={t(($) => $.account.profile_description_label)}
            description={t(($) => $.account.profile_description_hint)}
            size="text"
            align="start"
          >
            <div>
              <Textarea
                name="profile-description"
                autoComplete="off"
                aria-label={t(($) => $.account.profile_description_label)}
                value={profileDescription}
                onChange={(event) => setProfileDescription(event.target.value)}
                onBlur={autoSave.flush}
                placeholder={t(($) => $.account.profile_description_placeholder)}
                rows={5}
                maxLength={MAX_PROFILE_DESCRIPTION_LEN}
                aria-invalid={descriptionTooLong}
                className="resize-y"
              />
              <div className="mt-1 flex justify-end text-xs text-muted-foreground">
                <span
                  className={descriptionTooLong ? 'text-destructive shrink-0' : 'shrink-0'}
                  aria-live="polite"
                >
                  {profileDescription.length}/{MAX_PROFILE_DESCRIPTION_LEN}
                </span>
              </div>
              {descriptionTooLong ? (
                <p className="mt-1 text-xs text-destructive">
                  {t(($) => $.account.profile_description_too_long, {
                    max: MAX_PROFILE_DESCRIPTION_LEN,
                    count: profileDescription.length,
                  })}
                </p>
              ) : null}
            </div>
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      {kerberosSlot && hasKerberos === true ? (
        <SettingsSection title={t(($) => $.account.section_kerberos)}>
          {kerberosSlot}
        </SettingsSection>
      ) : null}

      {/* R-16g. The same destination the Help menu already offers, put where
          a person looks for it. `onboardingReplay()` only carries a marker
          that lifts the already-onboarded bounce; nothing is reset — not the
          questionnaire, not credentials, not workspaces — so this is a
          navigation and nothing else. */}
      <SettingsSection title={t(($) => $.account.section_onboarding)}>
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.account.replay_onboarding_label)}
            description={t(($) => $.account.replay_onboarding_hint)}
          >
            <Button variant="outline" onClick={() => navigation.push(paths.onboardingReplay())}>
              <RotateCcw className="size-3.5" />
              {t(($) => $.account.replay_onboarding_action)}
            </Button>
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}

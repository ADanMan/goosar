'use client';

import {
  ArrowUpRight,
  BookOpen,
  CircleHelp,
  MessageCircle,
  RotateCcw,
  Sparkles,
} from 'lucide-react';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@goosar/ui/components/ui/dropdown-menu';
import { useModalStore } from '@goosar/core/modals';
import { useConfigStore } from '@goosar/core/config';
import { docsUrl } from '@goosar/core/i18n';
import { paths, useWorkspaceSlug } from '@goosar/core/paths';
import { useNavigation } from '../navigation';
import { useT } from '../i18n';

export function HelpLauncher() {
  const { t, i18n } = useT('layout');
  const docsHref = docsUrl(i18n.language);
  const navigation = useNavigation();
  const workspaceSlug = useWorkspaceSlug();
  const serverVersion = useConfigStore((state) => state.serverVersion);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={t(($) => $.help.trigger)}
        title={t(($) => $.help.trigger)}
        className="inline-flex size-7 items-center justify-center rounded-full text-muted-foreground transition-colors cursor-pointer hover:bg-accent hover:text-foreground data-popup-open:bg-accent data-popup-open:text-foreground"
      >
        <CircleHelp className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side="top" sideOffset={8} className="min-w-40 max-w-56">
        {/* Role capabilities (#251). First item on purpose: "what can I
            actually do here" is the question a new member has before any
            documentation question, and this is the only permanent answer to
            it — the post-onboarding welcome dialog is one-shot. */}
        {workspaceSlug && (
          <DropdownMenuItem
            onClick={() => navigation.push(paths.workspace(workspaceSlug).capabilities())}
          >
            <Sparkles className="h-3.5 w-3.5" />
            {t(($) => $.help.capabilities)}
          </DropdownMenuItem>
        )}
        <DropdownMenuItem render={<a href={docsHref} target="_blank" rel="noopener noreferrer" />}>
          <BookOpen className="h-3.5 w-3.5" />
          {t(($) => $.help.docs)}
          <ArrowUpRight className="size-3 translate-y-px text-muted-foreground/50" />
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => useModalStore.getState().open('feedback')}>
          <MessageCircle className="h-3.5 w-3.5" />
          {t(($) => $.help.feedback)}
        </DropdownMenuItem>
        {/* Re-enter the shared onboarding flow. The desktop navigation
            adapter intercepts any /onboarding push (params included) into
            the onboarding WindowOverlay, where the flow is ungated; on web
            the /onboarding page reads the replay marker to skip the
            already-onboarded bounce. */}
        <DropdownMenuItem onClick={() => navigation.push(paths.onboardingReplay())}>
          <RotateCcw className="h-3.5 w-3.5" />
          {t(($) => $.help.replay_onboarding)}
        </DropdownMenuItem>
        {serverVersion && (
          <>
            <DropdownMenuSeparator />
            {/* DropdownMenuLabel renders Base UI's Menu.GroupLabel, which reads
                a Menu.Group context and throws if it has no Group ancestor. It
                must always be wrapped in a DropdownMenuGroup — without it the
                Help menu crashes the whole app on open (no error boundary sits
                above the sidebar). */}
            <DropdownMenuGroup>
              <DropdownMenuLabel className="font-normal break-words">
                {t(($) => $.help.server_version, { version: serverVersion })}
              </DropdownMenuLabel>
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

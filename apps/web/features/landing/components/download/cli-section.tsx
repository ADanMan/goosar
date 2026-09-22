'use client';

import { useEffect, useState } from 'react';
import { Check, Copy, Terminal } from 'lucide-react';
import { copyText } from '@goosar/ui/lib/clipboard';
import { useLocale } from '../../i18n';

const CLOUD_APP_URL = 'https://goosar.ru';

function installCommand(appUrl: string) {
  return `curl -fsSL ${appUrl}/install.sh | bash -s -- --app-url ${appUrl}`;
}

const SETUP_CMD = 'goosar setup';

export function CliSection() {
  const { t } = useLocale();
  const d = t.download.cli;
  const [appUrl, setAppUrl] = useState(CLOUD_APP_URL);
  useEffect(() => setAppUrl(window.location.origin), []);
  const installCmd = installCommand(appUrl);

  return (
    <section id="cli" className="bg-[#f7f7f5] py-20 text-[#1c1917] sm:py-24">
      <div className="mx-auto max-w-[820px] px-4 sm:px-6 lg:px-8">
        <h2 className="font-[family-name:var(--font-serif)] text-[2.2rem] leading-[1.1] tracking-[-0.03em] sm:text-[2.6rem]">
          {d.title}
        </h2>
        <p className="mt-4 max-w-[620px] text-[15px] leading-7 text-[#1c1917]/72">{d.sub}</p>

        <div className="mt-10 flex flex-col gap-5">
          <CommandBlock
            label={d.installLabel}
            cmd={installCmd}
            copyLabel={d.copyLabel}
            copiedLabel={d.copiedLabel}
          />
          <CommandBlock
            label={d.startLabel}
            cmd={SETUP_CMD}
            copyLabel={d.copyLabel}
            copiedLabel={d.copiedLabel}
          />
        </div>

        <p className="mt-6 text-[13px] text-[#1c1917]/60">{d.sshNote}</p>
      </div>
    </section>
  );
}

function CommandBlock({
  label,
  cmd,
  copyLabel,
  copiedLabel,
}: {
  label: string;
  cmd: string;
  copyLabel: string;
  copiedLabel: string;
}) {
  const [copied, setCopied] = useState(false);

  const onCopy = async () => {
    if (await copyText(cmd)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    }
  };

  return (
    <div>
      <p className="mb-2 text-[12px] font-medium uppercase tracking-[0.08em] text-[#1c1917]/55">
        {label}
      </p>
      <div className="flex items-start gap-3 rounded-xl border border-[#1c1917]/10 bg-white px-4 py-3 font-mono text-[13.5px]">
        <Terminal className="mt-0.5 size-4 shrink-0 text-[#1c1917]/55" aria-hidden />
        <code className="min-w-0 flex-1 whitespace-pre-wrap break-all">{cmd}</code>
        <button
          type="button"
          onClick={onCopy}
          aria-label={copied ? copiedLabel : copyLabel}
          className="inline-flex shrink-0 items-center gap-1.5 rounded-md px-2 py-1 text-[12px] font-medium text-[#1c1917]/70 transition-colors hover:bg-[#1c1917]/5 hover:text-[#1c1917]"
        >
          {copied ? (
            <>
              <Check className="size-3.5" />
              {copiedLabel}
            </>
          ) : (
            <>
              <Copy className="size-3.5" />
              {copyLabel}
            </>
          )}
        </button>
      </div>
    </div>
  );
}

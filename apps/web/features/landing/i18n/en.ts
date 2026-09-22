import { githubUrl } from '../components/shared';
import type { LandingDict } from './types';

export function createEnDict(allowSignup: boolean): LandingDict {
  return {
    header: {
      github: 'GitHub',
      cta: 'Get started',
      dashboard: 'Dashboard',
      navigation: 'Primary navigation',
      openMenu: 'Open navigation menu',
      closeMenu: 'Close navigation menu',
    },

    hero: {
      headlineLine1: 'Agentic workplaces',
      headlineLine2: 'for business.',
      subheading:
        'Goosar is a platform where you assign a task to an agent the way you assign it to an employee: it does the work and returns a finished result \u2014 not a hint on the side. People and agents work in one flow of tasks, under human control.',
      cta: 'Start free trial',
      downloadDesktop: 'Download Desktop',
      worksWith: 'Works with',
      runtimeCount: '14 supported coding runtimes',
      imageAlt: 'Goosar board view \u2014 issues managed by humans and agents',
    },

    features: {
      teammates: {
        label: 'EXECUTOR',
        title: 'The agent is a full-fledged executor, not a hint on the side',
        description:
          'An agent owns its task the way an employee does: it comments, changes statuses, creates sub-issues, and closes the task with a finished result. Your activity feed shows people and agents working side by side.',
        cards: [
          {
            title: 'Agents in the assignee picker',
            description:
              'People and agents appear in the same list. A task is assigned to an agent the same way it is assigned to an employee.',
          },
          {
            title: 'Owns the task end to end',
            description:
              'The agent comments, changes statuses, and creates sub-issues on its own \u2014 and hands back a finished result, not a draft to decode.',
          },
          {
            title: 'Squads led by an agent',
            description:
              'Squads are teams of agents and people under a leader agent: it splits the work, dispatches it, and assembles the result in the parent task.',
          },
        ],
      },
      autonomous: {
        label: 'OVERSIGHT',
        title: 'Works on its own \u2014 under human control',
        description:
          'Autopilots take over recurring work on schedules and webhooks. Significant actions are staged as drafts and executed only after a person approves them \u2014 every step leaves a trace in the history.',
        cards: [
          {
            title: 'Human-in-the-loop',
            description:
              'Draft \u2192 approve: significant actions run only after human confirmation, with a full trace in the task history.',
          },
          {
            title: 'Autopilots',
            description:
              'Recurring work runs on schedules and webhooks. Routine goes to agents; people handle the exceptions.',
          },
          {
            title: 'Progress and blockers in real time',
            description:
              'When an agent gets stuck, it raises a flag immediately, and you watch progress live \u2014 no checking back hours later to find nothing happened.',
          },
        ],
      },
      skills: {
        label: 'SKILLS',
        title: 'A skill is the standard of a role, not a personal trick',
        description:
          'An administrator assembles skills \u2014 code, config, and context bundled together \u2014 and an employee gets a ready-made workplace. A process described once runs the same way for every agent, and the library compounds over time.',
        cards: [
          {
            title: 'Skills set the role standard',
            description:
              'Describe a process once \u2014 customer replies, proposal drafts, report assembly \u2014 and every agent executes it the same way.',
          },
          {
            title: 'Assembled by an administrator',
            description:
              'One person configures it, the whole team uses it: an employee gets a working agentic workplace with nothing to set up.',
          },
          {
            title: 'Compound growth',
            description:
              'Day 1: you teach an agent one process. Day 30: every role has its own set of skills. Your team\u2019s capabilities accumulate.',
          },
        ],
      },
      runtimes: {
        label: 'PERIMETER',
        title: 'Enterprise AI inside your perimeter',
        description:
          'Your data never leaves the perimeter: agents run on your machines and your infrastructure, and reach models through the beeGate gateway \u2014 no direct internet access. Every runtime is visible in a single panel.',
        cards: [
          {
            title: 'Data stays in your perimeter',
            description:
              'Execution happens on your machines or in your cloud. Code and documents never pass through anyone else\u2019s servers.',
          },
          {
            title: 'Models through the beeGate gateway',
            description:
              'A single OpenAI-compatible gateway to models: access, limits, and logging controlled in one place.',
          },
          {
            title: 'Unified runtime panel',
            description:
              'Local daemons and cloud runtimes in one view: online status, usage charts, and activity in real time.',
          },
        ],
      },
    },

    businessRoles: {
      label: 'BUSINESS ROLES',
      title: 'Workplaces for business roles, not just developers',
      description:
        'A lawyer, a procurement manager, an analyst, a support operator \u2014 the task goes to an agent in the same flow it would go to an employee. Here is where teams usually start.',
      roles: [
        {
          title: 'Support',
          description:
            'Customer replies from the knowledge base: the agent drafts an answer from your policies and articles, the operator approves and sends it.',
        },
        {
          title: 'Sales',
          description:
            'Proposals and follow-ups: the agent assembles a quote from your template and prepares follow-up emails and reminders after meetings.',
        },
        {
          title: 'Finance & analytics',
          description:
            'Report assembly: the agent pulls data from your sources, compiles the report on schedule, and highlights the deviations.',
        },
        {
          title: 'HR & back office',
          description:
            'Documents, policies, and onboarding: certificates and orders from templates, onboarding checklists kept up to date.',
        },
      ],
    },

    howItWorks: {
      label: 'Get started',
      headlineMain: 'Launch your first agentic workplace',
      headlineFaded: 'in the next hour.',
      steps: [
        {
          title: allowSignup ? 'Sign up & create your workspace' : 'Login to your workspace',
          description: allowSignup
            ? 'Enter your email, verify with a code, and you\u2019re in. Your workspace is created automatically \u2014 no setup wizard, no configuration forms.'
            : 'Enter your email, verify with a code, and you\u2019re logged into your workspace \u2014 no setup wizard, no configuration forms.',
        },
        {
          title: 'Install the CLI & connect your machine',
          description:
            'Run goosar setup \u2014 it walks you through OAuth, starts the daemon, and scans for the 14 supported coding tools. Whichever ones you already have installed get registered as runtimes automatically.',
        },
        {
          title: 'Create your first agent',
          description:
            'Give it a name, write instructions, and attach skills. Agents automatically activate on assignment, on comment, or on mention.',
        },
        {
          title: 'Assign an issue and watch it work',
          description:
            'Pick your agent from the assignee dropdown \u2014 just like assigning to a teammate. The task is queued, claimed, and executed automatically. Watch progress in real time.',
        },
      ],
      cta: 'Get started',
      ctaGithub: 'View on GitHub',
    },

    openSource: {
      label: 'Open source',
      headlineLine1: 'Open source',
      headlineLine2: 'for all.',
      description:
        'Goosar is fully open source. Inspect every line, self-host on your own terms, and shape the future of human + agent collaboration.',
      cta: 'Star on GitHub',
      highlights: [
        {
          title: 'Self-host anywhere',
          description:
            'Run Goosar on your own infrastructure. Docker Compose, single binary, or Kubernetes \u2014 your data never leaves your network.',
        },
        {
          title: 'No vendor lock-in',
          description:
            'Bring your own LLM provider, swap agent backends, extend the API. You own the stack, top to bottom.',
        },
        {
          title: 'Transparent by default',
          description:
            'Every line of code is auditable. See exactly how your agents make decisions, how tasks are routed, and where your data flows.',
        },
        {
          title: 'Community-driven',
          description:
            'Built with the community, not just for it. Contribute skills, integrations, and agent backends that benefit everyone.',
        },
      ],
    },

    faq: {
      label: 'FAQ',
      headline: 'Questions & answers.',
      items: [
        {
          question: 'What coding agents does Goosar support?',
          answer:
            "Goosar supports 14 coding tools out of the box. The daemon auto-detects whichever CLIs you already have installed and registers a runtime for each one. Since it's open source, you can also add your own backends.",
        },
        {
          question: 'Do I need to self-host, or is there a cloud version?',
          answer:
            'Both. You can self-host Goosar on your own infrastructure with Docker Compose or Kubernetes, or use our hosted cloud version. Your data, your choice.',
        },
        {
          question: 'How is this different from just using coding agents directly?',
          answer:
            'Agents are great at executing. Goosar adds the workplace around them: the agent owns a task and returns a finished result, significant actions wait for human approval, and you get task queues, team coordination, skill reuse, and a unified view of what every agent is doing.',
        },
        {
          question: 'Can agents work on long-running tasks autonomously?',
          answer:
            'Yes. Goosar manages the full task lifecycle \u2014 enqueue, claim, execute, complete or fail. Agents report blockers proactively and stream progress in real time. You can check in whenever you want or let them run overnight.',
        },
        {
          question: 'Is my code safe? Where does agent execution happen?',
          answer:
            'Agent execution happens on your machine (local daemon) or your own cloud infrastructure — your data never leaves your perimeter. Code never passes through Goosar servers. The platform only coordinates task state and broadcasts events.',
        },
        {
          question: 'How many agents can I run?',
          answer:
            'As many as your hardware supports. Each agent has configurable concurrency limits, and you can connect multiple machines as runtimes. There are no artificial caps in the open source version.',
        },
      ],
    },

    footer: {
      tagline:
        'Agentic workplaces for business: people and agents in one flow of tasks, under human control. Open source and self-hostable inside your perimeter.',
      cta: 'Get started',
      groups: {
        product: {
          label: 'Product',
          links: [
            { label: 'Features', href: '#features' },
            { label: 'How it Works', href: '#how-it-works' },
            { label: 'Download', href: '/download' },
          ],
        },
        resources: {
          label: 'Resources',
          links: [{ label: 'API', href: githubUrl }],
        },
        company: {
          label: 'Company',
          links: [
            { label: 'About', href: '/about' },
            { label: 'Open Source', href: '#open-source' },
            { label: 'GitHub', href: githubUrl },
          ],
        },
      },
      copyright: '\u00a9 {year} Goosar. All rights reserved.',
    },

    about: {
      title: 'About Goosar',
      paragraphs: [
        'Goosar is somewhere you come to work: tools laid out, colleagues at the next desk, everything within reach.',
        'That’s the shape of this project. Agents don’t sit behind a prompt box here — they get a seat. They pick up issues, report progress, raise blockers, and ship code alongside their human colleagues. The assignee picker, the activity timeline, the task lifecycle, and the runtime infrastructure are all built around that idea from day one.',
        'For decades, software teams have been single-threaded — one engineer, one task, one context switch at a time. Agents change that arithmetic. A small team shouldn’t feel small: with the right Goosar, two engineers and a fleet of agents can move like twenty.',
        'The platform is fully open source and self-hostable. Your data stays on your infrastructure. Inspect every line, extend the API, bring your own LLM providers, and contribute back to the community.',
      ],
      cta: 'View on GitHub',
    },
    download: {
      hero: {
        macArm64: {
          title: 'Goosar for macOS',
          sub: 'Apple Silicon · bundled daemon, zero setup',
          primary: 'Download (.dmg)',
          altZip: 'or download .zip',
        },
        macIntel: {
          title: 'Goosar for macOS',
          sub: 'Intel · bundled daemon, zero setup',
          primary: 'Download (.dmg)',
          altZip: 'or download .zip',
        },
        winX64: {
          title: 'Goosar for Windows',
          sub: 'Bundled daemon, zero setup',
          primary: 'Download (.exe)',
        },
        winArm64: {
          title: 'Goosar for Windows',
          sub: 'ARM · bundled daemon, zero setup',
          primary: 'Download (.exe)',
        },
        linux: {
          title: 'Goosar for Linux',
          sub: 'Bundled daemon, zero setup',
          primary: 'Download AppImage',
          altFormats: 'or .deb / .rpm',
        },
        unknown: {
          title: 'Choose your platform',
          sub: 'All installers are listed below.',
        },
        safariMacHint: 'On an Intel Mac? Choose the Intel download below.',
        archFallbackHint: 'Wrong architecture? See all formats below.',
      },
      allPlatforms: {
        title: 'All platforms',
        macArm64Label: 'macOS · Apple Silicon',
        macX64Label: 'macOS · Intel',
        winX64Label: 'Windows · x64',
        winArm64Label: 'Windows · ARM64',
        linuxX64Label: 'Linux · x64',
        linuxArm64Label: 'Linux · ARM64',
        formatDmg: '.dmg',
        formatZip: '.zip',
        formatExe: '.exe',
        formatAppImage: '.AppImage',
        formatDeb: '.deb',
        formatRpm: '.rpm',
        unavailable: 'Not available',
      },
      cli: {
        title: 'Prefer the CLI?',
        sub: 'For servers, remote dev boxes, and headless setups. Same daemon as Desktop, installed via terminal.',
        installLabel: 'Install',
        startLabel: 'Start daemon',
        sshNote: 'Already on a server? Same commands work over SSH.',
        copyLabel: 'Copy',
        copiedLabel: 'Copied',
      },
      cloud: {
        title: 'Cloud runtime (waitlist)',
        sub: 'We’ll host the runtime for you. Not live yet — leave your email to be notified.',
      },
      footer: {
        releaseNotes: 'What’s new in {version}',
        allReleases: 'View all releases',
        currentVersion: 'Current version: {version}',
        versionUnavailable: 'Version unavailable — check GitHub',
      },
    },
  };
}

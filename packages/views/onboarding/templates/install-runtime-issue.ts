// Путь пропуска, задача 1/2: «Подключите runtime, чтобы начать работать
// с агентами» — туториал по установке.

export const INSTALL_RUNTIME_ISSUE_TITLE = {
  en: 'Step 1 — Connect a runtime to start using agents',
  zh: '第 1 步 —— 连接运行时,开始使用 agent',
  ko: '1단계 — agent를 사용하려면 runtime 연결하기',
  ja: 'ステップ1 — agent を使うために runtime を接続する',
  ru: 'Шаг 1 — Подключите среду выполнения, чтобы запустить агентов',
} as const;

const en = `Welcome to Goosar.

Agents need a runtime before they can execute work. You can still use Goosar as a lightweight project-management workspace while you install one.

## Try Goosar first

Before the runtime is ready, you can:

1. Create a project for your current work.
2. Create a few issues and move them across backlog, todo, in_progress, and done.
3. Add priorities, labels, comments, and subscriptions.
4. Use Inbox to track assignments and mentions.

That gives you the project-management layer first. Once a runtime is connected, agents can start working from the same issues.

## Install your first agent runtime

Full guide: https://goosar.ru/docs/install-agent-runtime

For English users, the fastest first path is Codex:

1. Make sure Node.js is installed.
2. Install Codex:
   npm i -g @openai/codex
3. Sign in:
   codex
4. Confirm your terminal can find it:
   which codex
   codex --version
5. Restart the Goosar daemon:
   goosar daemon restart
   If you use the desktop app, restarting the app is enough.
6. Return to Runtimes and refresh. You should see a Codex runtime online.
7. Create your first agent from that runtime, then assign an issue to the agent and set status to todo.

Codex reference: https://developers.openai.com/codex/cli

When the runtime is connected, you can create Goosar Helper for a guided first run.`;

const zh = `欢迎来到 Goosar。

智能体需要先连上运行时才能执行工作。运行时还没准备好时,你也可以先把 Goosar 当作轻量项目管理工具体验起来。

## 先体验项目管理功能

运行时安装前,你可以先做这些事:

1. 为当前工作创建一个项目。
2. 新建几个 issue,并在 backlog、todo、in_progress、done 之间流转。
3. 给 issue 加优先级、标签、评论和订阅。
4. 用收件箱追踪分配给你的事项和 @mention。

这样你先熟悉项目管理层。连上运行时后,智能体会直接在这些 issue 上开始工作。

## 安装第一个 Agent 运行时

完整文档:https://goosar.ru/docs/install-agent-runtime

中文用户建议先装 Kimi CLI:

1. 在 macOS / Linux 终端安装 Kimi CLI:
   curl -LsSf https://code.kimi.com/install.sh | bash
   Windows PowerShell:
   Invoke-RestMethod https://code.kimi.com/install.ps1 | Invoke-Expression
2. 确认终端能找到 Kimi:
   kimi --version
3. 在你想让 Kimi 工作的项目目录里启动一次:
   kimi
4. 首次启动后输入 /login,按提示完成 Kimi Code 或 API key 配置。
5. 重启 Goosar 守护进程:
   goosar daemon restart
   如果你用桌面端,重启 app 即可。
6. 回到 Runtimes 页面刷新。你应该能看到一个在线的 Kimi 运行时。
7. 用这个运行时创建第一个智能体,再把一个 issue 分配给它,并把状态切到 todo。

Kimi CLI 官方文档:https://moonshotai.github.io/kimi-cli/zh/guides/getting-started.html

运行时连上后,你就可以创建 Goosar Helper,开始一次有智能体参与的上手引导。`;

const ko = `Goosar에 오신 것을 환영합니다.

agent가 작업을 실행하려면 먼저 runtime이 필요합니다. runtime을 설치하는 동안에도 Goosar를 가벼운 프로젝트 관리 워크스페이스로 먼저 사용할 수 있습니다.

## 먼저 Goosar를 사용해 보기

runtime이 준비되기 전에는 다음을 해볼 수 있습니다:

1. 현재 작업을 위한 project를 만듭니다.
2. issue 몇 개를 만들고 backlog, todo, in_progress, done 사이에서 이동해 봅니다.
3. priority, label, comment, subscription을 추가합니다.
4. Inbox에서 나에게 배정된 작업과 mention을 확인합니다.

이렇게 프로젝트 관리 계층을 먼저 익힐 수 있습니다. runtime이 연결되면 agent가 같은 issue에서 바로 작업을 시작합니다.

## 첫 agent runtime 설치하기

전체 가이드: https://goosar.ru/docs/install-agent-runtime

한국어 사용자는 Codex로 시작하는 것이 가장 빠릅니다:

1. Node.js가 설치되어 있는지 확인합니다.
2. Codex를 설치합니다:
   npm i -g @openai/codex
3. 로그인합니다:
   codex
4. 터미널에서 찾을 수 있는지 확인합니다:
   which codex
   codex --version
5. Goosar daemon을 재시작합니다:
   goosar daemon restart
   데스크톱 앱을 사용한다면 앱을 재시작해도 됩니다.
6. Runtimes로 돌아가 새로고침합니다. Codex runtime이 online으로 보여야 합니다.
7. 해당 runtime으로 첫 agent를 만든 뒤 issue를 agent에게 배정하고 status를 todo로 바꿉니다.

Codex 참고 문서: https://developers.openai.com/codex/cli

runtime이 연결되면 Goosar Helper를 만들어 안내를 받으며 첫 실행을 시작할 수 있습니다.`;

const ja = `Goosar へようこそ。

agent が作業を実行するには、まず runtime が必要です。runtime をインストールしている間も、Goosar を軽量なプロジェクト管理ワークスペースとして先に使うことができます。

## まず Goosar を使ってみる

runtime が準備できる前に、次のことを試せます:

1. いまの仕事のための project を作る。
2. issue をいくつか作り、backlog、todo、in_progress、done の間で動かしてみる。
3. priority、label、comment、subscription を追加する。
4. Inbox で自分への割り当てや mention を確認する。

これでまずプロジェクト管理のレイヤーに慣れることができます。runtime を接続すると、agent が同じ issue から作業を始められます。

## 最初の agent runtime をインストールする

詳しいガイド: https://goosar.ru/docs/install-agent-runtime

日本語ユーザーには、Codex で始めるのが最も速い経路です:

1. Node.js がインストールされていることを確認します。
2. Codex をインストールします:
   npm i -g @openai/codex
3. サインインします:
   codex
4. ターミナルから見つけられるか確認します:
   which codex
   codex --version
5. Goosar daemon を再起動します:
   goosar daemon restart
   デスクトップアプリを使っている場合は、アプリを再起動するだけで十分です。
6. Runtimes に戻って再読み込みします。Codex runtime が online と表示されるはずです。
7. その runtime から最初の agent を作り、issue を agent に割り当てて status を todo にします。

Codex のリファレンス: https://developers.openai.com/codex/cli

runtime が接続されたら、Goosar Helper を作成して、案内付きの最初の実行を始められます。`;

const ru = `Добро пожаловать в Goosar.

Агентам нужна среда выполнения, чтобы выполнять работу. Пока вы её устанавливаете, Goosar можно использовать как лёгкий инструмент управления проектами.

## Сначала осмотритесь в Goosar

Пока среда выполнения не готова, можно:

1. Создать проект для текущей работы.
2. Создать несколько issue и провести их через backlog, todo, in_progress и done.
3. Добавить приоритеты, метки, комментарии и подписки.
4. Отслеживать назначения и упоминания во «Входящих».

Так вы сначала освоите слой управления проектами. Когда среда выполнения подключится, агенты начнут работать с теми же issue.

## Установите первую среду выполнения для агентов

Полное руководство: https://goosar.ru/docs/install-agent-runtime

Самый быстрый первый путь — Codex:

1. Убедитесь, что установлен Node.js.
2. Установите Codex:
   npm i -g @openai/codex
3. Войдите в аккаунт:
   codex
4. Проверьте, что терминал его находит:
   which codex
   codex --version
5. Перезапустите демон Goosar:
   goosar daemon restart
   Если вы пользуетесь десктопным приложением, достаточно перезапустить его.
6. Вернитесь в «Среды выполнения» и обновите страницу. Там должна появиться среда выполнения Codex со статусом online.
7. Создайте из неё первого агента, затем назначьте ему issue и поставьте статус todo.

Справочник Codex: https://developers.openai.com/codex/cli

Когда среда выполнения подключена, создайте Goosar Helper — он проведёт вас через первый запуск.`;

export const INSTALL_RUNTIME_ISSUE_BODY = { en, zh, ko, ja, ru } as const;

export const FOLLOWUP_COMMENT_PREFIX = {
  en: 'Your next step:',
  zh: '完成后的下一步：',
  ko: '다음 단계:',
  ja: '次のステップ:',
  ru: 'Следующий шаг:',
} as const;

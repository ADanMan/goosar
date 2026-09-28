// Команда goosar_admin — CLI администратора деплоя (docs/50-api-contract.md,
// «Приложение. CLI администратора деплоя»). Работает напрямую с БД сервера
// через DATABASE_URL (как и мигратор, server2/cmd/server -migrate) — это не
// клиент HTTP API, а серверная операторская утилита: список/подтверждение/
// отклонение заявок на роль deployment-admin, аварийная выдача роли,
// сборка мусора вложений, применение политики хранения, идемпотентный
// провижининг ролевых воркспейсов, ротация секретов, аварийный сброс MFA,
// заполнение библиотеки MCP-серверов деплоя.
//
// Собирается: `go build -o goosar_admin ./cmd/admin`. Эталонное поведение —
// вывод `--help` старого бинарника (`/tmp/cli-bin/goosar_admin --help`,
// исходники которого не читались — только эта справка, см. правила clean-room
// в server2/docs/decisions.md, раздел T-029): тот же набор команд и флагов,
// текст справки — своими словами.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/store"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usage = `goosar_admin — CLI администратора деплоя Goosar

Использование: goosar_admin <команда> [флаги]

Команды:
  list-pending                                Показать заявки на выдачу/отзыв роли deployment-admin, ждущие подтверждения
  confirm <request-id>                        Подтвердить одну ожидающую заявку
  reject <request-id>                         Отклонить одну ожидающую заявку
  grant <email>                                Аварийно выдать роль deployment-admin существующему пользователю, минуя заявку
  gc-uploads [--dry-run] [--grace=168h] [--limit=500]
                                                Удалить осиротевшие загрузки (не привязанные к задаче/комментарию/сообщению чата)
  purge [--dry-run] [--chat=720h] [--tasks=720h] [--closed-issues=8760h] [--activity=720h] [--attachment-grace=168h]
                                                Применить политику хранения по окнам
  provision-roles                              Идемпотентно создать ролевые воркспейсы деплоя из включённых шаблонов
  rotate-secrets --mcp [--mfa] [--dry-run]     Перешифровать значения, запечатанные GOOSAR_MCP_SECRET_KEY, текущим ключом
  mfa-reset <email>                            Аварийно сбросить второй фактор аккаунта и завершить его сессии
  mcp-library seed [--dry-run]                 Заполнить библиотеку MCP-серверов деплоя из GOOSAR_DEPLOYMENT_*_URL

Все команды (кроме --help без аргументов) требуют переменную окружения
DATABASE_URL, указывающую на уже мигрированную базу server2.
`

// run — вся логика main() вынесена в тестируемую функцию (io.Writer вместо
// os.Stdout/os.Stderr, возврат кода выхода вместо os.Exit).
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		fmt.Fprintln(stderr, "goosar_admin: команда обязательна")
		return 1
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "confirm", "reject":
		if len(rest) != 1 {
			fmt.Fprintf(stderr, "goosar_admin: %s требует ровно один аргумент <id>\n", cmd)
			return 1
		}
	case "grant", "mfa-reset":
		if len(rest) != 1 {
			fmt.Fprintf(stderr, "goosar_admin: %s требует ровно один аргумент\n", cmd)
			return 1
		}
	case "list-pending", "gc-uploads", "purge", "provision-roles", "rotate-secrets", "mcp-library":
		// флаги разбираются в dispatch, после подключения к БД.
	default:
		fmt.Fprintf(stderr, "goosar_admin: неизвестная команда %q\n", cmd)
		return 1
	}

	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(stderr, "DATABASE_URL не задан")
		return 1
	}
	ctx := context.Background()
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "не удалось открыть БД: %v\n", err)
		return 1
	}
	defer db.Close()

	if err := dispatch(ctx, stdout, db, cfg, cmd, rest); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", cmd, err)
		return 1
	}
	return 0
}

func dispatch(ctx context.Context, out io.Writer, db *store.Store, cfg config.Config, cmd string, args []string) error {
	switch cmd {
	case "list-pending":
		return runListPending(ctx, out, db)
	case "confirm":
		return runConfirm(ctx, out, db, args[0])
	case "reject":
		return runReject(ctx, out, db, args[0])
	case "grant":
		return runGrant(ctx, out, db, args[0])
	case "gc-uploads":
		opts, err := parseGCUploadsFlags(args, cfg.UploadGCGrace)
		if err != nil {
			return err
		}
		return runGCUploads(ctx, out, db, cfg, opts)
	case "purge":
		defaults := purgeOptions{
			chat:            cfg.RetentionChat,
			tasks:           cfg.RetentionTasks,
			closedIssues:    cfg.RetentionClosedIssues,
			activity:        cfg.RetentionActivity,
			attachmentGrace: cfg.AttachmentPurgeGrace,
		}
		opts, err := parsePurgeFlags(args, defaults)
		if err != nil {
			return err
		}
		return runPurge(ctx, out, db, opts)
	case "provision-roles":
		return runProvisionRoles(ctx, out, db, cfg)
	case "rotate-secrets":
		opts, err := parseRotateSecretsFlags(args)
		if err != nil {
			return err
		}
		return runRotateSecrets(ctx, out, db, cfg, opts)
	case "mfa-reset":
		return runMfaReset(ctx, out, db, authn.NewStore(db), args[0])
	case "mcp-library":
		if len(args) == 0 || args[0] != "seed" {
			return errors.New(`ожидается подкоманда "seed"`)
		}
		opts, err := parseMcpLibrarySeedFlags(args[1:])
		if err != nil {
			return err
		}
		return runMcpLibrarySeed(ctx, out, db, cfg, opts)
	default:
		return fmt.Errorf("неизвестная команда: %s", cmd)
	}
}

func durationHours(h int) time.Duration { return time.Duration(h) * time.Hour }

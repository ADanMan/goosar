package mail

import "fmt"

// Locale — язык письма. Контракт (docs/51-data-model.md, accounts.acct_locale)
// ограничивает acct_locale набором {en, zh-Hans, ko, ja, ru}; T-029 пишет
// собственный текст только для "ru" и общего английского фолбэка — остальные
// локали (zh-Hans/ko/ja) получают английский текст, а не машинный перевод
// (см. server2/docs/decisions.md, раздел T-029, «Пробелы спецификации»).
type Locale string

const (
	LocaleRU Locale = "ru"
	LocaleEN Locale = "en"
)

// LocaleFrom сводит произвольную строку (acct_locale аккаунта, "" если ещё
// не выбрана) к одной из двух поддерживаемых локалей письма.
func LocaleFrom(s string) Locale {
	if s == "ru" {
		return LocaleRU
	}
	return LocaleEN
}

// LoginCodeMessage — письмо, отправляемое POST /auth/send-code: 6-значный
// код плюс magic-link на тот же вход (contract: "Создаёт ... 6-значный код +
// magic-link токен ... отправляет письмо"). linkURL пусто, если magic-link
// в этом деплое не строится (например FRONTEND_ORIGIN не задан) — тогда
// письмо ограничивается кодом.
func LoginCodeMessage(to string, locale Locale, code, linkURL string) Message {
	if locale == LocaleRU {
		body := fmt.Sprintf("Код для входа в Goosar: %s\n\nОн действует 10 минут.", code)
		if linkURL != "" {
			body += fmt.Sprintf("\n\nИли перейдите по ссылке, чтобы войти без ввода кода:\n%s", linkURL)
		}
		body += "\n\nЕсли вы не запрашивали вход, просто проигнорируйте это письмо."
		return Message{To: to, Subject: "Код для входа в Goosar", Body: body}
	}
	body := fmt.Sprintf("Your Goosar sign-in code: %s\n\nIt expires in 10 minutes.", code)
	if linkURL != "" {
		body += fmt.Sprintf("\n\nOr use this link to sign in without typing the code:\n%s", linkURL)
	}
	body += "\n\nIf you didn't request this, you can safely ignore this email."
	return Message{To: to, Subject: "Your Goosar sign-in code", Body: body}
}

// InviteMessage — письмо-приглашение в рабочее пространство. Не используется
// сегодняшним internal/workspace (он шлёт собственный короткий текст — не
// пакет mail, а домен workspace, вне T-029), но нужен как готовый шаблон для
// будущей унификации и для тестов транспорта этой сессии.
func InviteMessage(to string, locale Locale, inviterName, workspaceName, acceptURL string) Message {
	if locale == LocaleRU {
		return Message{
			To:      to,
			Subject: fmt.Sprintf("%s приглашает вас в «%s» на Goosar", inviterName, workspaceName),
			Body: fmt.Sprintf(
				"%s приглашает вас присоединиться к рабочему пространству «%s» на Goosar.\n\nПринять приглашение:\n%s",
				inviterName, workspaceName, acceptURL),
		}
	}
	return Message{
		To:      to,
		Subject: fmt.Sprintf("%s invited you to “%s” on Goosar", inviterName, workspaceName),
		Body: fmt.Sprintf(
			"%s invited you to join the “%s” workspace on Goosar.\n\nAccept the invitation:\n%s",
			inviterName, workspaceName, acceptURL),
	}
}

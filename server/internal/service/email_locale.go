package service

import (
	"fmt"
	"html"
	"strings"

	"github.com/resend/resend-go/v2"
)

const (
	EmailLangRU = "ru"
	EmailLangEN = "en"
)

func emailLangForTag(tag string) string {
	primary := strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(primary, "-_"); i >= 0 {
		primary = primary[:i]
	}
	switch primary {
	case "ru":
		return EmailLangRU
	case "en":
		return EmailLangEN
	default:
		return ""
	}
}

func EmailLangForUserLanguage(stored string) string {
	if lang := emailLangForTag(stored); lang != "" {
		return lang
	}
	return EmailLangRU
}

func buildVerificationEmail(code, webURL, deepURL, lang string) (subject, htmlBody string) {
	safeWeb := html.EscapeString(webURL)
	safeDeep := html.EscapeString(deepURL)

	if lang == EmailLangEN {
		linkSection := ""
		if webURL != "" {
			linkSection = fmt.Sprintf(
				`<p style="margin: 24px 0;">
					<a href="%s" style="display: inline-block; padding: 12px 24px; background: #000; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500;">Sign in</a>
				</p>
				<p style="color: #666; font-size: 14px;">The link works once: with the desktop app installed it opens the app signed in, otherwise it takes you to the web sign-in. If the app didn't open, use <a href="%s">this direct link</a>.</p>
				<p style="color: #666; font-size: 14px;">Or enter the code manually:</p>`, safeWeb, safeDeep)
		}
		return "Your Goosar verification code", fmt.Sprintf(
			`<div style="font-family: sans-serif; max-width: 400px; margin: 0 auto;">
				<h2>Your verification code</h2>%s
				<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0;">%s</p>
				<p>This code expires in 10 minutes.</p>
				<p style="color: #666; font-size: 14px;">If you didn't request this code, you can safely ignore this email.</p>
			</div>`, linkSection, code)
	}

	linkSection := ""
	if webURL != "" {
		linkSection = fmt.Sprintf(
			`<p style="margin: 24px 0;">
				<a href="%s" style="display: inline-block; padding: 12px 24px; background: #000; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500;">Войти</a>
			</p>
			<p style="color: #666; font-size: 14px;">Ссылка работает один раз: если установлено приложение, она откроет его с выполненным входом, иначе приведёт ко входу через браузер. Если приложение не открылось, воспользуйтесь <a href="%s">прямой ссылкой</a>.</p>
			<p style="color: #666; font-size: 14px;">Или введите код вручную:</p>`, safeWeb, safeDeep)
	}
	return "Код подтверждения Goosar", fmt.Sprintf(
		`<div style="font-family: sans-serif; max-width: 400px; margin: 0 auto;">
			<h2>Ваш код подтверждения</h2>%s
			<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0;">%s</p>
			<p>Код действует 10 минут.</p>
			<p style="color: #666; font-size: 14px;">Если вы не запрашивали код, просто проигнорируйте это письмо.</p>
		</div>`, linkSection, code)
}

func buildInvitationParams(from, to, inviterName, workspaceName, inviteURL, lang string) *resend.SendEmailRequest {
	safeWorkspace := html.EscapeString(workspaceName)
	safeInviter := html.EscapeString(inviterName)
	subjectInviter := sanitizeSubjectField(inviterName)
	subjectWorkspace := sanitizeSubjectField(workspaceName)

	var subject, body string
	if lang == EmailLangEN {
		subject = fmt.Sprintf("%s invited you to %s on Goosar", subjectInviter, subjectWorkspace)
		action := `<p style="color: #666; font-size: 14px;">Open Goosar and sign in to accept or decline the invitation.</p>`
		if inviteURL != "" {
			action = fmt.Sprintf(
				`<p style="margin: 24px 0;">
					<a href="%s" style="display: inline-block; padding: 12px 24px; background: #000; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500;">Accept invitation</a>
				</p>
				<p style="color: #666; font-size: 14px;">You'll need to log in to accept or decline the invitation.</p>`,
				html.EscapeString(inviteURL))
		}
		body = fmt.Sprintf(
			`<div style="font-family: sans-serif; max-width: 480px; margin: 0 auto;">
				<h2>You're invited to join %s</h2>
				<p><strong>%s</strong> invited you to collaborate in the <strong>%s</strong> workspace on Goosar.</p>
				%s
			</div>`, safeWorkspace, safeInviter, safeWorkspace, action)
	} else {
		subject = fmt.Sprintf("%s приглашает вас в рабочее пространство «%s» в Goosar", subjectInviter, subjectWorkspace)
		action := `<p style="color: #666; font-size: 14px;">Откройте Goosar и войдите, чтобы принять или отклонить приглашение.</p>`
		if inviteURL != "" {
			action = fmt.Sprintf(
				`<p style="margin: 24px 0;">
					<a href="%s" style="display: inline-block; padding: 12px 24px; background: #000; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500;">Принять приглашение</a>
				</p>
				<p style="color: #666; font-size: 14px;">Чтобы принять или отклонить приглашение, войдите в Goosar.</p>`,
				html.EscapeString(inviteURL))
		}
		body = fmt.Sprintf(
			`<div style="font-family: sans-serif; max-width: 480px; margin: 0 auto;">
				<h2>Вас приглашают в рабочее пространство «%s»</h2>
				<p><strong>%s</strong> приглашает вас присоединиться к «<strong>%s</strong>» в Goosar.</p>
				%s
			</div>`, safeWorkspace, safeInviter, safeWorkspace, action)
	}

	return &resend.SendEmailRequest{
		From:    from,
		To:      []string{to},
		Subject: subject,
		Html:    body,
	}
}

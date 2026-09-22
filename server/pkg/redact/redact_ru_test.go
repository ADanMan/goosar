package redact

import (
	"strings"
	"testing"
)

func TestRedactRussianFormats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		input       string
		gone        string
		placeholder string
	}{
		{"phone plus7 grouped", "звони +7 912 345-67-89 днём", "912", "[REDACTED PHONE]"},
		{"phone plus7 solid", "+79123456789", "9123456789", "[REDACTED PHONE]"},
		{"phone plus7 parens", "тел. +7 (912) 345-67-89", "345-67-89", "[REDACTED PHONE]"},
		{"phone leading eight", "8 912 345 67 89 — мобильный", "345 67 89", "[REDACTED PHONE]"},
		{"phone leading eight solid", "89123456789", "89123456789", "[REDACTED PHONE]"},

		{"inn legal entity", "ИНН 7707083893 КПП 773601001", "7707083893", "[REDACTED INN]"},
		{"inn person 12 digits", "ИНН: 500100732259", "500100732259", "[REDACTED INN]"},
		{"inn latin label", "INN=7707083893", "7707083893", "[REDACTED INN]"},

		{"snils spaced", "СНИЛС 112-233-445 95", "112-233-445", "[REDACTED SNILS]"},
		{"snils dashed", "112-233-445-95", "112-233-445", "[REDACTED SNILS]"},

		{"passport spaced", "паспорт 4509 123456 выдан", "4509 123456", "[REDACTED PASSPORT]"},
		{"passport solid", "Passport 4509123456", "4509123456", "[REDACTED PASSPORT]"},
		{"passport series label", "серия 4509 номер 123456", "123456", "[REDACTED PASSPORT]"},

		{"card visa spaced", "оплата 4111 1111 1111 1111", "4111", "[REDACTED CARD]"},
		{"card mastercard solid", "5555555555554444", "5555555555554444", "[REDACTED CARD]"},
		{"card mir dashed", "2200-0000-0000-0053", "2200", "[REDACTED CARD]"},

		{"cyrillic email", "написал иван.петров@почта.рф вчера", "иван.петров", "[REDACTED EMAIL]"},
		{"cyrillic local ascii domain", "пользователь@example.com", "пользователь", "[REDACTED EMAIL]"},

		{"yandex cloud api key", "key AQVNxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "AQVNxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "[REDACTED YANDEX KEY]"},
		{"gigachat secret", "GIGACHAT_CLIENT_SECRET=abc-123-def", "abc-123-def", "[REDACTED"},
		{"bitrix webhook", "хук https://acme.bitrix24.ru/rest/12/abcdef0123456789/", "abcdef0123456789", "[REDACTED BITRIX WEBHOOK]"},
		{"kerberos keytab", "KRB5_CLIENT_KTNAME=/etc/krb5.keytab", "/etc/krb5.keytab", "[REDACTED KERBEROS]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Text(tc.input)
			if strings.Contains(got, tc.gone) {
				t.Fatalf("%q survived redaction: %s", tc.gone, got)
			}
			if !strings.Contains(got, tc.placeholder) {
				t.Fatalf("expected %s in output, got: %s", tc.placeholder, got)
			}
		})
	}
}

func TestRedactRussianFormats_FalsePositivesBounded(t *testing.T) {
	t.Parallel()
	kept := []struct {
		name  string
		input string
	}{
		{"migration number", "applied migration 0296_auth_audit_action_index"},
		{"bare ten digit id", "task 7707083893 finished"},
		{"bare twelve digit id", "trace 500100732259 completed"},
		{"unix millis timestamp", "at 1757328000123 the daemon reconnected"},
		{"sixteen digit non-luhn", "request 1234567890123456 retried"},
		{"port and duration", "listening on 127.0.0.1:8081 for 12345 ms"},
		{"semver and sha", "v1.0.0 commit 9d13c1e built 2026-09-08"},
		{"ascii email untouched", "ivan.petrov@example.com opened the issue"},
		{"four then six digits without label", "counts 4509 123456 rows"},
	}
	for _, tc := range kept {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Text(tc.input); got != tc.input {
				t.Fatalf("input was modified\n  in:  %s\n  out: %s", tc.input, got)
			}
		})
	}
}

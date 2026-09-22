package handler

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func userLanguage(lang string) pgtype.Text {
	return pgtype.Text{String: lang, Valid: true}
}

func TestNoRuntimeIssueDescription_PicksContentLanguage(t *testing.T) {
	tests := []struct {
		name     string
		language pgtype.Text

		wantMarker string
	}{
		{"no stored language falls back to the product default", pgtype.Text{}, "Добро пожаловать в Goosar."},
		{"empty stored language falls back to the product default", userLanguage(""), "Добро пожаловать в Goosar."},
		{"ru", userLanguage("ru"), "Добро пожаловать в Goosar."},
		{"ru-RU region tag", userLanguage("ru-RU"), "Добро пожаловать в Goosar."},
		{"en", userLanguage("en"), "Welcome to Goosar."},
		{"en-US region tag", userLanguage("en-US"), "Welcome to Goosar."},
		{"zh", userLanguage("zh"), "欢迎来到 Goosar。"},
		{"zh-Hans", userLanguage("zh-Hans"), "欢迎来到 Goosar。"},
		{"zh-CN", userLanguage("zh-CN"), "欢迎来到 Goosar。"},
		{"locale with no shim copy falls back to the product default", userLanguage("ko"), "Добро пожаловать в Goosar."},
		{"ja falls back to the product default", userLanguage("ja"), "Добро пожаловать в Goosar."},
		{"unsupported locale falls back to the product default", userLanguage("fr-FR"), "Добро пожаловать в Goosar."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := noRuntimeIssueDescription(tt.language)
			if !strings.Contains(got, tt.wantMarker) {
				t.Errorf("noRuntimeIssueDescription(%q) missing %q\ngot first line: %q",
					tt.language.String, tt.wantMarker, strings.SplitN(got, "\n", 2)[0])
			}
		})
	}
}

func TestOnboardingShimTitlesStayStableForDedupe(t *testing.T) {
	if onboardingIssueTitle != "Start here: learn Goosar with Goosar Helper" {
		t.Errorf("onboardingIssueTitle changed to %q — pre-v3 dedupe matches on the exact string", onboardingIssueTitle)
	}
	if noRuntimeIssueTitle != "Connect a runtime to start using agents" {
		t.Errorf("noRuntimeIssueTitle changed to %q — pre-v3 dedupe matches on the exact string", noRuntimeIssueTitle)
	}
}

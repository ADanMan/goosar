package mail

import (
	"strings"
	"testing"
)

func TestLoginCodeMessageLocales(t *testing.T) {
	cases := []struct {
		name        string
		locale      Locale
		code, link  string
		wantSubject string
		mustContain []string
		mustOmit    []string
	}{
		{
			name: "ru with link", locale: LocaleRU, code: "123456", link: "https://app.test/login/verify?token=abc",
			wantSubject: "Goosar",
			mustContain: []string{"123456", "https://app.test/login/verify?token=abc"},
		},
		{
			name: "en without link", locale: LocaleEN, code: "654321", link: "",
			mustContain: []string{"654321"},
			mustOmit:    []string{"http"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := LoginCodeMessage("a@b.test", c.locale, c.code, c.link)
			if msg.To != "a@b.test" {
				t.Errorf("To = %q", msg.To)
			}
			if c.wantSubject != "" && !strings.Contains(msg.Subject, c.wantSubject) {
				t.Errorf("subject = %q, want it to contain %q", msg.Subject, c.wantSubject)
			}
			for _, want := range c.mustContain {
				if !strings.Contains(msg.Body, want) {
					t.Errorf("body %q missing %q", msg.Body, want)
				}
			}
			for _, omit := range c.mustOmit {
				if strings.Contains(msg.Body, omit) {
					t.Errorf("body %q should not contain %q", msg.Body, omit)
				}
			}
		})
	}
}

func TestLocaleFrom(t *testing.T) {
	fallbacks := []string{"", "en", "ja", "ko", "zh-Hans"}
	if LocaleFrom("ru") != LocaleRU {
		t.Error("ru должно распознаваться")
	}
	for _, s := range fallbacks {
		if LocaleFrom(s) != LocaleEN {
			t.Errorf("LocaleFrom(%q) должно откатываться к английскому", s)
		}
	}
}

func TestInviteMessage(t *testing.T) {
	for _, c := range []struct {
		locale             Locale
		inviter, workspace string
	}{
		{LocaleRU, "Алиса", "Ракета"},
		{LocaleEN, "Alice", "Rocket"},
	} {
		msg := InviteMessage("bob@example.test", c.locale, c.inviter, c.workspace, "https://app.test/invite/1")
		for _, want := range []string{c.inviter, c.workspace, "https://app.test/invite/1"} {
			if !strings.Contains(msg.Body, want) {
				t.Errorf("[%s] invite body %q missing %q", c.locale, msg.Body, want)
			}
		}
	}
}

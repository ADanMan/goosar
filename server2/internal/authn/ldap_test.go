package authn

import (
	"context"
	"net"
	"testing"

	"github.com/adanman/goosar/server2/internal/config"
)

// fakeLDAPServer — минимальный сервер LDAP на net.Listener: понимает ровно
// последовательность loginLDAP (service bind, поиск по одному
// equality-фильтру, user bind), достаточно, чтобы протестировать протокол
// (ldap_ber.go + ldap.go) без реального каталога — см.
// server2/docs/adr/0002-auth-providers.md, "тест — юнит на слое маппинга (и
// протокола, поднятого на net.Listener, раз без живого LDAP нельзя иначе)".
type fakeLDAPServer struct {
	serviceDN, servicePW string
	userDN, userPW       string
	userAttrs            map[string][]string
	searchAttr           string
	searchValue          string
}

// newFakeLDAPServer принимает два TCP-соединения по очереди — ровно
// столько, сколько открывает loginLDAP на стороне клиента (одно для service
// bind + поиска, второе — для финального bind паролем пользователя);
// единственный Accept() навсегда заблокировал бы второе соединение.
func newFakeLDAPServer(t *testing.T, s fakeLDAPServer) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn1, err := ln.Accept()
		if err != nil {
			return
		}
		s.serveSearchConn(t, conn1)
		conn1.Close()

		conn2, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn2.Close()
		s.handleBind(t, conn2, s.userDN, s.userPW)
	}()
	return "ldap://" + ln.Addr().String()
}

func (s fakeLDAPServer) serveSearchConn(t *testing.T, conn net.Conn) {
	// 1. service bind
	if !s.handleBind(t, conn, s.serviceDN, s.servicePW) {
		return
	}
	// 2. search
	top, err := readBERMessage(conn)
	if err != nil {
		t.Errorf("fake ldap: чтение SearchRequest: %v", err)
		return
	}
	children, err := berParse(top.data)
	if err != nil || len(children) < 2 || children[1].tag != berAppSearchRequest {
		t.Errorf("fake ldap: ожидался SearchRequest, получено %+v (err=%v)", children, err)
		return
	}
	reqFields, err := berParse(children[1].data)
	if err != nil || len(reqFields) < 7 {
		t.Errorf("fake ldap: SearchRequest: не удалось разобрать поля: %v", err)
		return
	}
	filterFields, err := berParse(reqFields[6].data)
	if err != nil || len(filterFields) < 2 {
		t.Errorf("fake ldap: SearchRequest: фильтр не разобрался: %v", err)
		return
	}
	gotAttr, gotValue := string(filterFields[0].data), string(filterFields[1].data)
	if gotAttr != s.searchAttr || gotValue != s.searchValue {
		t.Errorf("fake ldap: фильтр = (%s=%s), ожидалось (%s=%s)", gotAttr, gotValue, s.searchAttr, s.searchValue)
	}

	msgID := berPosIntValue(children[0].data)
	if gotValue == s.searchValue && gotAttr == s.searchAttr {
		entryOp := berTLV(berAppSearchEntry, concatBytes(
			berOctetString(berTagOctetString, s.userDN),
			berTLV(berSeq, encodeAttributes(s.userAttrs)),
		))
		writeMsg(conn, msgID, entryOp)
	}
	doneOp := berTLV(berAppSearchDone, concatBytes(
		berPosInt(berTagEnumerated, 0),
		berOctetString(berTagOctetString, ""),
		berOctetString(berTagOctetString, ""),
	))
	writeMsg(conn, msgID, doneOp)
}

func (s fakeLDAPServer) handleBind(t *testing.T, conn net.Conn, wantDN, wantPW string) bool {
	top, err := readBERMessage(conn)
	if err != nil {
		t.Errorf("fake ldap: чтение BindRequest: %v", err)
		return false
	}
	children, err := berParse(top.data)
	if err != nil || len(children) < 2 || children[1].tag != berAppBindRequest {
		t.Errorf("fake ldap: ожидался BindRequest: %+v (err=%v)", children, err)
		return false
	}
	bindFields, err := berParse(children[1].data)
	if err != nil || len(bindFields) < 3 {
		t.Errorf("fake ldap: BindRequest: не удалось разобрать поля: %v", err)
		return false
	}
	dn, pw := string(bindFields[1].data), string(bindFields[2].data)
	msgID := berPosIntValue(children[0].data)

	result := 0
	if dn != wantDN || pw != wantPW {
		result = 49 // invalidCredentials
	}
	op := berTLV(berAppBindResponse, concatBytes(
		berPosInt(berTagEnumerated, result),
		berOctetString(berTagOctetString, ""),
		berOctetString(berTagOctetString, ""),
	))
	writeMsg(conn, msgID, op)
	return result == 0
}

func writeMsg(conn net.Conn, msgID int, op []byte) {
	msg := berTLV(berSeq, concatBytes(berPosInt(berTagInteger, msgID), op))
	_, _ = conn.Write(msg)
}

func encodeAttributes(attrs map[string][]string) []byte {
	var out []byte
	for name, vals := range attrs {
		var valSet []byte
		for _, v := range vals {
			valSet = append(valSet, berOctetString(berTagOctetString, v)...)
		}
		partial := berTLV(berSeq, concatBytes(
			berOctetString(berTagOctetString, name),
			berTLV(0x31, valSet), // SET OF (universal 17, constructed => 0x31)
		))
		out = append(out, partial...)
	}
	return out
}

func TestLoginLDAPSuccess(t *testing.T) {
	srv := fakeLDAPServer{
		serviceDN: "cn=svc,dc=example,dc=test", servicePW: "svc-secret",
		userDN: "uid=bob,ou=people,dc=example,dc=test", userPW: "bob-password",
		userAttrs:   map[string][]string{"mail": {"bob@example.test"}, "cn": {"Bob Bobson"}},
		searchAttr:  "uid",
		searchValue: "bob",
	}
	addr := newFakeLDAPServer(t, srv)

	cfg := config.LDAPConfig{
		URL: addr, BindDN: srv.serviceDN, BindPassword: srv.servicePW,
		BaseDN: "ou=people,dc=example,dc=test", UserFilter: "(uid=%s)",
		EmailAttr: "mail", NameAttr: "cn",
	}
	dn, email, name, err := loginLDAP(context.Background(), cfg, "bob", "bob-password")
	if err != nil {
		t.Fatalf("loginLDAP: unexpected error: %v", err)
	}
	if dn != srv.userDN {
		t.Errorf("dn = %q, want %q", dn, srv.userDN)
	}
	if email != "bob@example.test" {
		t.Errorf("email = %q", email)
	}
	if name != "Bob Bobson" {
		t.Errorf("name = %q", name)
	}
}

func TestLoginLDAPWrongPassword(t *testing.T) {
	srv := fakeLDAPServer{
		serviceDN: "cn=svc,dc=example,dc=test", servicePW: "svc-secret",
		userDN: "uid=bob,ou=people,dc=example,dc=test", userPW: "bob-password",
		userAttrs:   map[string][]string{"mail": {"bob@example.test"}, "cn": {"Bob"}},
		searchAttr:  "uid",
		searchValue: "bob",
	}
	addr := newFakeLDAPServer(t, srv)
	cfg := config.LDAPConfig{
		URL: addr, BindDN: srv.serviceDN, BindPassword: srv.servicePW,
		BaseDN: "ou=people,dc=example,dc=test", UserFilter: "(uid=%s)",
		EmailAttr: "mail", NameAttr: "cn",
	}
	_, _, _, err := loginLDAP(context.Background(), cfg, "bob", "wrong-password")
	if err == nil {
		t.Fatal("loginLDAP: expected an error for the wrong password")
	}
}

func TestParseEqualityFilterTemplate(t *testing.T) {
	cases := []struct {
		tmpl    string
		attr    string
		wantErr bool
	}{
		{"(uid=%s)", "uid", false},
		{"(sAMAccountName=%s)", "sAMAccountName", false},
		{"uid=%s", "", true},
		{"(uid=*%s*)", "", true},
		{"(uid)", "", true},
	}
	for _, c := range cases {
		attr, err := parseEqualityFilterTemplate(c.tmpl)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseEqualityFilterTemplate(%q): expected error", c.tmpl)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseEqualityFilterTemplate(%q): unexpected error: %v", c.tmpl, err)
		}
		if attr != c.attr {
			t.Errorf("parseEqualityFilterTemplate(%q) = %q, want %q", c.tmpl, attr, c.attr)
		}
	}
}

func TestLdapEscapeFilterValue(t *testing.T) {
	got := ldapEscapeFilterValue(`a*b(c)d\e`)
	want := `a\2ab\28c\29d\5ce`
	if got != want {
		t.Errorf("ldapEscapeFilterValue = %q, want %q", got, want)
	}
}

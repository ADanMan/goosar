package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterHandleStubYieldsToDomain(t *testing.T) {
	r := New()
	r.Handle(http.MethodGet, "/api/widgets", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("domain"))
	})
	// Заглушка на тот же метод+путь не должна вытеснить домен.
	r.HandleStub(http.MethodGet, "/api/widgets", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})
	// Заглушка на свободный путь должна зарегистрироваться.
	r.HandleStub(http.MethodGet, "/api/gizmos", WriteNotImplemented)

	req := httptest.NewRequest(http.MethodGet, "/api/widgets", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "domain" {
		t.Fatalf("ожидался ответ домена (200 domain), получено %d %q", rec.Code, rec.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/gizmos", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotImplemented {
		t.Fatalf("ожидался 501 на заглушке, получено %d", rec2.Code)
	}

	registered := r.Registered()
	if len(registered) != 2 {
		t.Fatalf("ожидалось 2 зарегистрированных маршрута, получено %d: %v", len(registered), registered)
	}
}

func TestRouterHandlePanicsOnDuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ожидалась паника при повторной регистрации одного маршрута двумя доменами")
		}
	}()
	r := New()
	r.Handle(http.MethodPost, "/api/widgets", WriteNotImplemented)
	r.Handle(http.MethodPost, "/api/widgets", WriteNotImplemented)
}

func TestIsRegistered(t *testing.T) {
	r := New()
	if r.IsRegistered(http.MethodGet, "/x") {
		t.Fatal("не должно быть зарегистрировано до вызова Handle")
	}
	r.Handle(http.MethodGet, "/x", WriteNotImplemented)
	if !r.IsRegistered(http.MethodGet, "/x") {
		t.Fatal("должно быть зарегистрировано после Handle")
	}
}

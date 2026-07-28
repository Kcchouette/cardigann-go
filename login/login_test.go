package login

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
)

func TestLogin_FormSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_ = r.ParseForm()
			if r.FormValue("username") == "alice" && r.FormValue("password") == "secret" {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
				w.Write([]byte(`<html><body>Bienvenue</body></html>`))
				return
			}
			w.Write([]byte(`<html><body><div class="error">Invalid credentials</div></body></html>`))
		case "/profile":
			if _, err := r.Cookie("session"); err == nil {
				w.Write([]byte(`<html><body><div class="logged-in">Mon compte</div></body></html>`))
				return
			}
			w.Write([]byte(`<html><body>Not logged in</body></html>`))
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method: "form",
			Login:  "/login",
			Inputs: map[string]string{
				"username": "{{ .Config.username }}",
				"password": "{{ .Config.password }}",
			},
			Error: []cmodel.ErrorSelector{{Selector: "div.error"}},
			Test:  &cmodel.Test{Path: "/profile", Selector: "div.logged-in"},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{
		"username": "alice",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("expected successful login, got error: %v", err)
	}
}

func TestLogin_FormWrongCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Write([]byte(`<html><body><div class="error">Invalid credentials</div></body></html>`))
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method: "form",
			Login:  "/login",
			Inputs: map[string]string{
				"username": "{{ .Config.username }}",
				"password": "{{ .Config.password }}",
			},
			Error: []cmodel.ErrorSelector{{Selector: "div.error"}},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{
		"username": "bob",
		"password": "wrong",
	})
	if err == nil {
		t.Fatal("expected login error for wrong credentials")
	}
	if _, ok := err.(*ErrLoginFailed); !ok {
		t.Errorf("expected *ErrLoginFailed, got %T: %v", err, err)
	}
}

func TestLogin_CookieMethod(t *testing.T) {
	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{Method: "cookie"},
	}
	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, "https://example.invalid", map[string]string{
		"cookie": "session=abc123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLogin_GetWithInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		_ = r.ParseForm()
		if r.FormValue("action") == "latest" && r.FormValue("passkey") == "abc123" {
			w.Write([]byte(`<html><body>OK</body></html>`))
			return
		}
		w.Write([]byte(`<html><body><div class="error">bad params</div></body></html>`))
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method: "get",
			Login:  "/api",
			Inputs: map[string]string{
				"action":  "latest",
				"passkey": "{{ .Config.passkey }}",
			},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{
		"passkey": "abc123",
	})
	if err != nil {
		t.Fatalf("expected successful login, got error: %v", err)
	}
}

func TestLogin_GetWithErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div class="error">Invalid API key</div></body></html>`))
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method: "get",
			Login:  "/api",
			Inputs: map[string]string{
				"passkey": "{{ .Config.passkey }}",
			},
			Error: []cmodel.ErrorSelector{{Selector: "div.error"}},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{
		"passkey": "bad",
	})
	if err == nil {
		t.Fatal("expected login error for bad passkey")
	}
	if _, ok := err.(*ErrLoginFailed); !ok {
		t.Errorf("expected *ErrLoginFailed, got %T: %v", err, err)
	}
}

func TestLogin_GetWithoutInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Write([]byte(`<html><body>OK</body></html>`))
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method: "get",
			Login:  "/api/test",
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{})
	if err != nil {
		t.Fatalf("expected successful login, got error: %v", err)
	}
}

func TestLogin_GetWithTestVerification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			w.Write([]byte(`<html><body>OK</body></html>`))
		case "/api/status":
			w.Write([]byte(`<html><body><div class="auth">Authenticated</div></body></html>`))
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method: "get",
			Login:  "/api",
			Test:   &cmodel.Test{Path: "/api/status", Selector: "div.auth"},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{})
	if err != nil {
		t.Fatalf("expected successful login with test verification, got error: %v", err)
	}
}

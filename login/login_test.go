package login

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
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

// TestLogin_CookieMethod_ConcurrentNilJar reproduces the scenario where multiple
// concurrent Login("cookie") calls share the same *http.Client without a
// pre-existing Jar: loginCookie must create the default jar without data race
// (verified by `go test -race`) or cookie loss.
func TestLogin_CookieMethod_ConcurrentNilJar(t *testing.T) {
	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{Method: "cookie"},
	}
	client := &http.Client{} // Jar nil intentionnellement

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Login(context.Background(), client, def, "https://example.invalid", map[string]string{
				"cookie": "session=abc123",
			}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if client.Jar == nil {
		t.Fatal("expected client.Jar to be initialized after concurrent logins")
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

func TestLogin_FormWithSelectorInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /login":
			w.Write([]byte(`<html><body>
				<form method="post">
					<input type="hidden" name="csrf_token" value="abc123secret"/>
					<input type="text" name="username"/>
					<input type="password" name="password"/>
				</form>
			</body></html>`))
		case "POST /login":
			_ = r.ParseForm()
			csrf := r.FormValue("csrf_token")
			user := r.FormValue("username")
			pass := r.FormValue("password")
			if csrf == "abc123secret" && user == "alice" && pass == "secret" {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
				w.Write([]byte(`<html><body>Bienvenue</body></html>`))
				return
			}
			w.Write([]byte(`<html><body><div class="error">Invalid</div></body></html>`))
		case "GET /profile":
			if _, err := r.Cookie("session"); err == nil {
				w.Write([]byte(`<html><body><div class="logged-in">OK</div></body></html>`))
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
			SelectorInputs: map[string]cmodel.SelectorField{
				"csrf_token": {Selector: "input[name=csrf_token]", Attribute: "value"},
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
		t.Fatalf("expected successful login with CSRF token, got error: %v", err)
	}
}

func TestLogin_FormWithSelectorInputsFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /login":
			w.Write([]byte(`<html><body>
				<script>var token = "tok_987654321";</script>
				<form method="post">
					<input type="hidden" name="csrf_token" value=""/>
				</form>
			</body></html>`))
		case "POST /login":
			_ = r.ParseForm()
			csrf := r.FormValue("csrf_token")
			user := r.FormValue("username")
			if csrf == "tok_987654321" && user == "alice" {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
				w.Write([]byte(`<html><body>OK</body></html>`))
				return
			}
			w.Write([]byte(`<html><body><div class="error">Missing CSRF</div></body></html>`))
		case "GET /profile":
			if _, err := r.Cookie("session"); err == nil {
				w.Write([]byte(`<html><body><div class="logged-in">OK</div></body></html>`))
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
			},
			SelectorInputs: map[string]cmodel.SelectorField{
				"csrf_token": {
					Selector: "script",
					Filters: []cmodel.FilterStep{
						{Name: "regexp", Args: []string{`token = "([^"]+)"`}},
					},
				},
			},
			Error: []cmodel.ErrorSelector{{Selector: "div.error"}},
			Test:  &cmodel.Test{Path: "/profile", Selector: "div.logged-in"},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{
		"username": "alice",
	})
	if err != nil {
		t.Fatalf("expected successful login with filtered CSRF token, got error: %v", err)
	}
}

func TestLogin_FormWithoutSelectorInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /login":
			_ = r.ParseForm()
			if r.FormValue("username") == "bob" && r.FormValue("password") == "pass" {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
				w.Write([]byte(`<html><body>OK</body></html>`))
				return
			}
			w.Write([]byte(`<html><body><div class="error">Bad</div></body></html>`))
		case "GET /profile":
			if _, err := r.Cookie("session"); err == nil {
				w.Write([]byte(`<html><body><div class="logged-in">OK</div></body></html>`))
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
		"username": "bob",
		"password": "pass",
	})
	if err != nil {
		t.Fatalf("expected successful login without selectorinputs, got error: %v", err)
	}
}

func TestLogin_FormWithOptionalSelectorInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /login":
			// Page without any CSRF token — optional selector should not fail
			w.Write([]byte(`<html><body>
				<form method="post">
					<input type="text" name="username"/>
				</form>
			</body></html>`))
		case "POST /login":
			_ = r.ParseForm()
			if r.FormValue("username") == "alice" {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
				w.Write([]byte(`<html><body>OK</body></html>`))
				return
			}
			w.Write([]byte(`<html><body><div class="error">Bad</div></body></html>`))
		case "GET /profile":
			if _, err := r.Cookie("session"); err == nil {
				w.Write([]byte(`<html><body><div class="logged-in">OK</div></body></html>`))
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
			},
			SelectorInputs: map[string]cmodel.SelectorField{
				"csrf_token": {Selector: "input[name=csrf_token]", Attribute: "value", Optional: true},
			},
			Error: []cmodel.ErrorSelector{{Selector: "div.error"}},
			Test:  &cmodel.Test{Path: "/profile", Selector: "div.logged-in"},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{
		"username": "alice",
	})
	if err != nil {
		t.Fatalf("expected successful login with optional missing selectorinputs, got error: %v", err)
	}
}

func TestLogin_OneURLSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("expected no query params, got %q", r.URL.RawQuery)
		}
		w.Write([]byte(`<html><body>OK</body></html>`))
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method: "oneurl",
			Login:  "/login/token",
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{})
	if err != nil {
		t.Fatalf("expected successful oneurl login, got error: %v", err)
	}
}

func TestLogin_OneURLErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div class="error">Access denied</div></body></html>`))
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method: "oneurl",
			Login:  "/login/token",
			Error:  []cmodel.ErrorSelector{{Selector: "div.error"}},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{})
	if err == nil {
		t.Fatal("expected login error for oneurl with error selector match")
	}
	if _, ok := err.(*ErrLoginFailed); !ok {
		t.Errorf("expected *ErrLoginFailed, got %T: %v", err, err)
	}
}

func TestLogin_FormWithHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" && r.Method == http.MethodPost {
			if r.Header.Get("X-Custom") != "test-value" {
				t.Errorf("expected X-Custom=test-value, got %q", r.Header.Get("X-Custom"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
			w.Write([]byte(`<html><body>OK</body></html>`))
			return
		}
		w.Write([]byte(`<html><body><div class="error">unexpected</div></body></html>`))
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
			Headers: map[string]string{
				"X-Custom": "test-value",
			},
			Error: []cmodel.ErrorSelector{{Selector: "div.error"}},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{
		"username": "alice",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("expected successful login with headers, got error: %v", err)
	}
}

func TestLogin_FormWithCookies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" && r.Method == http.MethodPost {
			cookie, err := r.Cookie("mycookie")
			if err != nil || cookie.Value != "myvalue" {
				t.Errorf("expected cookie mycookie=myvalue, got err=%v value=%q", err, cookie)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
			w.Write([]byte(`<html><body>OK</body></html>`))
			return
		}
		w.Write([]byte(`<html><body><div class="error">unexpected</div></body></html>`))
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
			Cookies: map[string]string{
				"mycookie": "myvalue",
			},
			Error: []cmodel.ErrorSelector{{Selector: "div.error"}},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{
		"username": "alice",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("expected successful login with cookies, got error: %v", err)
	}
}

func TestLogin_FormWithSubmitPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login-page":
			w.Write([]byte(`<html><body>login form</body></html>`))
		case "/submit":
			if r.Method == http.MethodPost {
				_ = r.ParseForm()
				if r.FormValue("username") == "alice" {
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
					w.Write([]byte(`<html><body>OK</body></html>`))
					return
				}
			}
			w.Write([]byte(`<html><body><div class="error">bad</div></body></html>`))
		default:
			w.Write([]byte(`<html><body><div class="error">unexpected path</div></body></html>`))
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method:     "form",
			Login:      "/login-page",
			SubmitPath: "/submit",
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
		"username": "alice",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("expected successful login with SubmitPath, got error: %v", err)
	}
}

func TestLogin_FormWithExplicitSelector(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login-page":
			w.Write([]byte(`<html><body>
				<form id="register" action="/register">
					<input name="email" />
				</form>
				<form id="login-form" action="/auth">
					<input name="username" />
					<input name="password" />
					<input type="hidden" name="csrf" value="tok123" />
				</form>
			</body></html>`))
		case "/auth":
			_ = r.ParseForm()
			if r.FormValue("username") == "alice" && r.FormValue("password") == "secret" && r.FormValue("csrf") == "tok123" {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
				w.Write([]byte(`<html><body>OK</body></html>`))
				return
			}
			w.Write([]byte(`<html><body><div class="error">bad</div></body></html>`))
		default:
			w.Write([]byte(`<html><body><div class="error">wrong path: ` + r.URL.Path + `</div></body></html>`))
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method: "form",
			Login:  "/login-page",
			Form:   "#login-form",
			Inputs: map[string]string{
				"username": "{{ .Config.username }}",
				"password": "{{ .Config.password }}",
			},
			SelectorInputs: map[string]cmodel.SelectorField{
				"csrf": {Selector: "input[name=csrf]", Attribute: "value"},
			},
			Error: []cmodel.ErrorSelector{{Selector: "div.error"}},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{
		"username": "alice",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("expected successful login with form selector, got error: %v", err)
	}
}

func TestLogin_FormSelectorNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>
			<form id="register" action="/register">
				<input name="email" />
			</form>
		</body></html>`))
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method: "form",
			Login:  "/login-page",
			Form:   "#nonexistent",
			Inputs: map[string]string{
				"username": "user",
				"password": "pass",
			},
		},
	}

	client := &http.Client{}
	client.Jar, _ = newJarForTest()

	err := Login(context.Background(), client, def, server.URL, map[string]string{
		"username": "user",
		"password": "pass",
	})
	if err == nil {
		t.Fatal("expected error for unmatched form selector")
	}
}

func TestLogin_FormSelectorEmpty(t *testing.T) {
	// Same as TestLogin_FormSuccess but with explicit empty Form field
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_ = r.ParseForm()
			if r.FormValue("username") == "alice" && r.FormValue("password") == "secret" {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
				w.Write([]byte(`<html><body>OK</body></html>`))
				return
			}
			w.Write([]byte(`<html><body><div class="error">bad</div></body></html>`))
		case "/profile":
			if _, err := r.Cookie("session"); err == nil {
				w.Write([]byte(`<html><body><div class="logged-in">OK</div></body></html>`))
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
			Form:   "",
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
		t.Fatalf("expected successful login with empty Form, got error: %v", err)
	}
}

func TestLogin_FormWithSubmitPathOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login-page":
			w.Write([]byte(`<html><body>
				<form id="login-form" action="/auth">
					<input name="username" />
					<input name="password" />
				</form>
			</body></html>`))
		case "/override-submit":
			_ = r.ParseForm()
			if r.FormValue("username") == "alice" && r.FormValue("password") == "secret" {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
				w.Write([]byte(`<html><body>OK</body></html>`))
				return
			}
			w.Write([]byte(`<html><body><div class="error">bad</div></body></html>`))
		default:
			w.Write([]byte(`<html><body><div class="error">wrong path: ` + r.URL.Path + `</div></body></html>`))
		}
	}))
	defer server.Close()

	def := cmodel.IndexerDefinition{
		Login: cmodel.Login{
			Method:     "form",
			Login:      "/login-page",
			Form:       "#login-form",
			SubmitPath: "/override-submit",
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
		"username": "alice",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("expected SubmitPath to override form action, got error: %v", err)
	}
}

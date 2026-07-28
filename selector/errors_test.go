package selector

import (
	"strings"
	"testing"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
)

func TestCheckErrors_NoSelectors(t *testing.T) {
	err := CheckErrors("<html><body>ok</body></html>", nil)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestCheckErrors_NonHTMLBody(t *testing.T) {
	err := CheckErrors(`{"error":"something"}`, []cmodel.ErrorSelector{
		{Selector: ".error"},
	})
	if err != nil {
		t.Errorf("expected nil for non-HTML body, got %v", err)
	}
}

func TestCheckErrors_NoMatch(t *testing.T) {
	body := `<html><body><div class="content">All good</div></body></html>`
	err := CheckErrors(body, []cmodel.ErrorSelector{
		{Selector: ".error-box"},
	})
	if err != nil {
		t.Errorf("expected nil when selector doesn't match, got %v", err)
	}
}

func TestCheckErrors_MatchDefaultMessage(t *testing.T) {
	body := `<html><body><div class="error-box">Your account is banned</div></body></html>`
	err := CheckErrors(body, []cmodel.ErrorSelector{
		{Selector: ".error-box"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Your account is banned") {
		t.Errorf("expected error to contain 'Your account is banned', got %q", err.Error())
	}
}

func TestCheckErrors_MatchMessageSelector(t *testing.T) {
	body := `<html><body>
		<div class="error-box">
			<span class="msg">Login credentials invalid</span>
		</div>
	</body></html>`
	err := CheckErrors(body, []cmodel.ErrorSelector{
		{Selector: ".error-box", Message: cmodel.MessageSelector{Selector: ".msg"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Login credentials invalid") {
		t.Errorf("expected error to contain 'Login credentials invalid', got %q", err.Error())
	}
}

func TestCheckErrors_MultipleSelectors_SecondMatches(t *testing.T) {
	body := `<html><body>
		<div class="notice">Welcome</div>
		<div class="error-box">Rate limit exceeded</div>
	</body></html>`
	err := CheckErrors(body, []cmodel.ErrorSelector{
		{Selector: ".notice"},
		{Selector: ".error-box"},
	})
	// .notice is not an error selector that returns a match error — the first one
	// should be skipped if it doesn't match, or if it does match, it returns error.
	// Since .notice matches, it returns error with "Welcome".
	if err == nil {
		t.Fatal("expected error from first matching selector, got nil")
	}
	if !strings.Contains(err.Error(), "Welcome") {
		t.Errorf("expected 'Welcome', got %q", err.Error())
	}
}

func TestCheckErrors_EmptyBody(t *testing.T) {
	err := CheckErrors("", []cmodel.ErrorSelector{
		{Selector: ".error"},
	})
	if err != nil {
		t.Errorf("expected nil for empty body, got %v", err)
	}
}

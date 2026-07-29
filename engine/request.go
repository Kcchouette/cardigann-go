package engine

import (
	"fmt"
	"net/url"
	"strings"

	tmpl "github.com/Kcchouette/cardigann-go/template"
)

// renderInputs renders all inputs via the template and separates $raw.
// Returns the rendered inputs and the raw $raw parameters.
func renderInputs(inputs map[string]string, rctx tmpl.Context) (map[string]string, string, error) {
	rendered := make(map[string]string, len(inputs))
	var raw string
	for k, v := range inputs {
		if k == "$raw" {
			renderedVal, err := tmpl.Render(v, rctx)
			if err != nil {
				return nil, "", fmt.Errorf("rendering $raw: %w", err)
			}
			raw = renderedVal
			continue
		}
		renderedVal, err := tmpl.Render(v, rctx)
		if err != nil {
			return nil, "", fmt.Errorf("rendering input %q: %w", k, err)
		}
		rendered[k] = renderedVal
	}
	return rendered, raw, nil
}

// appendRawParams adds the encoded $raw parameters to a url.Values.
func appendRawParams(values url.Values, raw string) {
	for _, param := range strings.Split(raw, "&") {
		if param == "" {
			continue
		}
		parts := strings.SplitN(param, "=", 2)
		if len(parts) == 2 {
			values.Add(parts[0], parts[1])
		}
	}
}

// filterInputs removes empty and "false" values (truthy in Go templates).
func filterInputs(rendered map[string]string) url.Values {
	values := url.Values{}
	for k, v := range rendered {
		// Skip empty or "false" parameters (avoids 400 errors on some APIs)
		// Note: "false" is truthy in Go templates, but APIs do not expect the parameter
		if v != "" && v != "false" {
			values.Set(k, v)
		}
	}
	return values
}

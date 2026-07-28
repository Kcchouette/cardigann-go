package engine

import (
	"fmt"
	"net/url"
	"strings"

	tmpl "github.com/Kcchouette/cardigann-go/template"
)

// renderInputs rend tous les inputs via le template et sépare $raw.
// Retourne les inputs rendus et les paramètres $raw bruts.
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

// appendRawParams ajoute les paramètres $raw encodés à une url.Values.
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

// filterInputs supprime les valeurs vides et "false" (truthy dans Go templates).
func filterInputs(rendered map[string]string) url.Values {
	values := url.Values{}
	for k, v := range rendered {
		// Ignorer les paramètres vides ou "false" (évite les erreurs 400 sur certaines APIs)
		// Note: "false" est truthy dans les templates Go, mais les APIs attendent pas de paramètre
		if v != "" && v != "false" {
			values.Set(k, v)
		}
	}
	return values
}

package selector

import (
	"fmt"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
	tmpl "github.com/Kcchouette/cardigann-go/template"
)

// CheckErrors inspects an HTML body against a list of error selectors.
// If any selector matches, it returns an error with the extracted message.
// rctx is threaded through for template-rendered error messages (message.text).
func CheckErrors(body string, selectors []cmodel.ErrorSelector, rctx tmpl.Context) error {
	if len(selectors) == 0 {
		return nil
	}
	doc, err := NewHTMLDocument(body)
	if err != nil {
		return nil // non-HTML body, cannot check
	}
	for _, es := range selectors {
		nodes, err := doc.Find(es.Selector)
		if err != nil || len(nodes) == 0 {
			continue
		}
		msg := "error detected"
		switch {
		case es.Message.Text != "":
			// message.text is a literal or template; render it with the context.
			if rendered, err := tmpl.Render(es.Message.Text, rctx); err == nil {
				msg = rendered
			}
		case es.Message.Selector != "":
			if msgNodes, err := doc.Find(es.Message.Selector); err == nil && len(msgNodes) > 0 {
				msg = msgNodes[0].Text()
			}
		default:
			msg = nodes[0].Text()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

package selector

import (
	"fmt"

	cmodel "github.com/Kcchouette/cardigann-go/definition"
)

// CheckErrors inspects an HTML body against a list of error selectors.
// If any selector matches, it returns an error with the extracted message.
func CheckErrors(body string, selectors []cmodel.ErrorSelector) error {
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
		if es.Message.Selector != "" {
			if msgNodes, err := doc.Find(es.Message.Selector); err == nil && len(msgNodes) > 0 {
				msg = msgNodes[0].Text()
			}
		} else {
			msg = nodes[0].Text()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

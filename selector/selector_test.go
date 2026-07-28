package selector

import "testing"

func TestHTMLDocument_FindAndSubSelect(t *testing.T) {
	doc, err := NewHTMLDocument(`
		<table>
			<tr class="row">
				<td><a class="title" href="/details/1">Release One</a></td>
			</tr>
			<tr class="row">
				<td><a class="title" href="/details/2">Release Two</a></td>
			</tr>
		</table>
	`)
	if err != nil {
		t.Fatalf("NewHTMLDocument: %v", err)
	}

	rows, err := doc.Find("tr.row")
	if err != nil {
		t.Fatalf("Find rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	sub, ok := rows[0].(SubSelectable)
	if !ok {
		t.Fatal("expected row node to implement SubSelectable")
	}
	titleNodes, err := sub.Find("a.title")
	if err != nil {
		t.Fatalf("sub Find: %v", err)
	}
	if len(titleNodes) != 1 {
		t.Fatalf("expected 1 title node, got %d", len(titleNodes))
	}
	if titleNodes[0].Text() != "Release One" {
		t.Errorf("unexpected text: %q", titleNodes[0].Text())
	}
	href, ok := titleNodes[0].Attr("href")
	if !ok || href != "/details/1" {
		t.Errorf("unexpected href: %q (ok=%v)", href, ok)
	}
}

func TestHTMLDocument_NoMatch(t *testing.T) {
	doc, err := NewHTMLDocument(`<html><body></body></html>`)
	if err != nil {
		t.Fatalf("NewHTMLDocument: %v", err)
	}
	nodes, err := doc.Find(".does-not-exist")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestJSONDocument_FindArray(t *testing.T) {
	doc, err := NewJSONDocument(`{"results":[{"title":"A","size":100},{"title":"B","size":200}]}`)
	if err != nil {
		t.Fatalf("NewJSONDocument: %v", err)
	}
	nodes, err := doc.Find("results")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	title, ok := nodes[0].Attr("title")
	if !ok || title != "A" {
		t.Errorf("unexpected title attr: %q (ok=%v)", title, ok)
	}
}

func TestJSONDocument_InvalidJSON(t *testing.T) {
	_, err := NewJSONDocument(`not json`)
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestJSONDocument_MissingPath(t *testing.T) {
	doc, err := NewJSONDocument(`{"a":1}`)
	if err != nil {
		t.Fatalf("NewJSONDocument: %v", err)
	}
	nodes, err := doc.Find("b")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if nodes != nil {
		t.Errorf("expected nil nodes for missing path, got %v", nodes)
	}
}

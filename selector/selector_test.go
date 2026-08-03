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

func TestHTMLDocument_AttrDecodesEntities(t *testing.T) {
	doc, err := NewHTMLDocument(`
		<form id="login" action="./ucp.php?mode=login&amp;sid=abc123">
			<input type="hidden" name="form_token" value="a&amp;b&lt;c" />
			<a href="./viewtopic.php?f=5&amp;t=42">topic</a>
		</form>
	`)
	if err != nil {
		t.Fatalf("NewHTMLDocument: %v", err)
	}
	formNodes, err := doc.Find("form#login")
	if err != nil || len(formNodes) != 1 {
		t.Fatalf("Find form: nodes=%d err=%v", len(formNodes), err)
	}
	action, ok := formNodes[0].Attr("action")
	if !ok || action != "./ucp.php?mode=login&sid=abc123" {
		t.Errorf("action not unescaped: %q (ok=%v)", action, ok)
	}
	anchorNodes, err := doc.Find("a[href*='viewtopic']")
	if err != nil || len(anchorNodes) != 1 {
		t.Fatalf("Find anchor: nodes=%d err=%v", len(anchorNodes), err)
	}
	href, ok := anchorNodes[0].Attr("href")
	if !ok || href != "./viewtopic.php?f=5&t=42" {
		t.Errorf("href not unescaped: %q (ok=%v)", href, ok)
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

func TestJSONDocument_SubSelect(t *testing.T) {
	doc, err := NewJSONDocument(`{
		"results": [
			{"title": "Alpha", "links": {"details": "/dl/1", "download": "/dl/1.torrent"}},
			{"title": "Beta",  "links": {"details": "/dl/2", "download": "/dl/2.torrent"}}
		]
	}`)
	if err != nil {
		t.Fatalf("NewJSONDocument: %v", err)
	}

	rows, err := doc.Find("results")
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

	// Sub-select scalar field
	titleNodes, err := sub.Find("title")
	if err != nil {
		t.Fatalf("sub Find title: %v", err)
	}
	if len(titleNodes) != 1 {
		t.Fatalf("expected 1 title node, got %d", len(titleNodes))
	}
	if titleNodes[0].Text() != "Alpha" {
		t.Errorf("unexpected title text: %q", titleNodes[0].Text())
	}

	// Sub-select nested object
	linkNodes, err := sub.Find("links")
	if err != nil {
		t.Fatalf("sub Find links: %v", err)
	}
	if len(linkNodes) != 1 {
		t.Fatalf("expected 1 link node, got %d", len(linkNodes))
	}
	detail, ok := linkNodes[0].Attr("details")
	if !ok || detail != "/dl/1" {
		t.Errorf("unexpected details attr: %q (ok=%v)", detail, ok)
	}

	// Sub-select via dot-path into nested object
	dlNodes, err := sub.Find("links.download")
	if err != nil {
		t.Fatalf("sub Find links.download: %v", err)
	}
	if len(dlNodes) != 1 {
		t.Fatalf("expected 1 download node, got %d", len(dlNodes))
	}
	if dlNodes[0].Text() != "/dl/1.torrent" {
		t.Errorf("unexpected download text: %q", dlNodes[0].Text())
	}

	// Sub-select nonexistent path
	missing, err := sub.Find("nonexistent")
	if err != nil {
		t.Fatalf("sub Find nonexistent: %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for nonexistent path, got %v", missing)
	}
}

func TestJSONDocument_SubSelectArray(t *testing.T) {
	doc, err := NewJSONDocument(`{
		"items": [
			{"tags": ["action", "drama"]},
			{"tags": ["comedy"]}
		]
	}`)
	if err != nil {
		t.Fatalf("NewJSONDocument: %v", err)
	}

	rows, err := doc.Find("items")
	if err != nil {
		t.Fatalf("Find items: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	sub0, ok := rows[0].(SubSelectable)
	if !ok {
		t.Fatal("expected row 0 to implement SubSelectable")
	}
	tagNodes, err := sub0.Find("tags")
	if err != nil {
		t.Fatalf("sub Find tags: %v", err)
	}
	if len(tagNodes) != 2 {
		t.Fatalf("expected 2 tag nodes, got %d", len(tagNodes))
	}
	if tagNodes[0].Text() != "action" {
		t.Errorf("unexpected tag[0]: %q", tagNodes[0].Text())
	}
	if tagNodes[1].Text() != "drama" {
		t.Errorf("unexpected tag[1]: %q", tagNodes[1].Text())
	}

	sub1, ok := rows[1].(SubSelectable)
	if !ok {
		t.Fatal("expected row 1 to implement SubSelectable")
	}
	tagNodes1, err := sub1.Find("tags")
	if err != nil {
		t.Fatalf("sub Find tags row1: %v", err)
	}
	if len(tagNodes1) != 1 {
		t.Fatalf("expected 1 tag node for row 1, got %d", len(tagNodes1))
	}
	if tagNodes1[0].Text() != "comedy" {
		t.Errorf("unexpected tag: %q", tagNodes1[0].Text())
	}
}

func TestJSONDocument_DollarSelector(t *testing.T) {
	// Test $ selector returns all array items
	doc, err := NewJSONDocument(`[{"name":"First","size":100},{"name":"Second","size":200}]`)
	if err != nil {
		t.Fatalf("NewJSONDocument: %v", err)
	}

	nodes, err := doc.Find("$")
	if err != nil {
		t.Fatalf("Find $: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	if nodes[0].Text() != `{"name":"First","size":100}` {
		t.Errorf("unexpected first node text: %q", nodes[0].Text())
	}
	if nodes[1].Text() != `{"name":"Second","size":200}` {
		t.Errorf("unexpected second node text: %q", nodes[1].Text())
	}
}

func TestJSONDocument_DollarSelector_Object(t *testing.T) {
	// Test $ selector on a single object (not array)
	doc, err := NewJSONDocument(`{"name":"Single","value":42}`)
	if err != nil {
		t.Fatalf("NewJSONDocument: %v", err)
	}

	nodes, err := doc.Find("$")
	if err != nil {
		t.Fatalf("Find $: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	if nodes[0].Text() != `{"name":"Single","value":42}` {
		t.Errorf("unexpected node text: %q", nodes[0].Text())
	}
}

func TestJSONDocument_DollarSelector_NestedArray(t *testing.T) {
	// Test $ selector on nested array
	doc, err := NewJSONDocument(`{"items":[{"id":1},{"id":2},{"id":3}]}`)
	if err != nil {
		t.Fatalf("NewJSONDocument: %v", err)
	}

	nodes, err := doc.Find("items")
	if err != nil {
		t.Fatalf("Find items: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
}

func TestJSONDocument_Attr(t *testing.T) {
	// Test Attr method on JSON nodes
	doc, err := NewJSONDocument(`[{"name":"Test","size":12345}]`)
	if err != nil {
		t.Fatalf("NewJSONDocument: %v", err)
	}

	nodes, err := doc.Find("$")
	if err != nil {
		t.Fatalf("Find $: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	name, ok := nodes[0].Attr("name")
	if !ok || name != "Test" {
		t.Errorf("Attr(name) = %q, %v, want 'Test', true", name, ok)
	}

	size, ok := nodes[0].Attr("size")
	if !ok || size != "12345" {
		t.Errorf("Attr(size) = %q, %v, want '12345', true", size, ok)
	}

	_, ok = nodes[0].Attr("nonexistent")
	if ok {
		t.Error("expected Attr(nonexistent) to return false")
	}
}

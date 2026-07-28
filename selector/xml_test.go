package selector

import (
	"strings"
	"testing"
)

const testTorznabXML = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:torznab="http://torznab.github.io/schemas/2015/feed">
  <channel>
    <title>Torznab Indexer</title>
    <atom:link href="http://localhost/api" rel="self" type="application/rss+xml"/>
    <item>
      <title>Ubuntu 22.04 LTS</title>
      <category>Linux</category>
      <pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate>
      <link>http://example.com/details/1</link>
      <guid isPermaLink="false">1</guid>
      <enclosure url="http://example.com/download/1.torrent" length="1024" type="application/x-bittorrent"/>
      <comments>http://example.com/comments/1</comments>
      <torznab:attr name="seeders" value="10"/>
      <torznab:attr name="peers" value="20"/>
      <torznab:attr name="size" value="1024000"/>
      <torznab:attr name="category" value="Linux"/>
    </item>
    <item>
      <title>Debian 12 Bookworm</title>
      <category>Linux</category>
      <pubDate>Tue, 02 Jan 2024 12:00:00 GMT</pubDate>
      <link>http://example.com/details/2</link>
      <guid isPermaLink="false">2</guid>
      <enclosure url="http://example.com/download/2.torrent" length="2048" type="application/x-bittorrent"/>
      <comments>http://example.com/comments/2</comments>
      <torznab:attr name="seeders" value="5"/>
      <torznab:attr name="peers" value="15"/>
      <torznab:attr name="size" value="2048000"/>
      <torznab:attr name="category" value="Linux"/>
    </item>
  </channel>
</rss>`

func TestXMLDocument_FindAndSubSelect(t *testing.T) {
	doc, err := NewXMLDocument(testTorznabXML)
	if err != nil {
		t.Fatalf("NewXMLDocument: %v", err)
	}

	rows, err := doc.Find("rss > channel > item")
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

	titleNodes, err := sub.Find("title")
	if err != nil {
		t.Fatalf("sub Find title: %v", err)
	}
	if len(titleNodes) != 1 {
		t.Fatalf("expected 1 title node, got %d", len(titleNodes))
	}
	if titleNodes[0].Text() != "Ubuntu 22.04 LTS" {
		t.Errorf("unexpected title text: %q", titleNodes[0].Text())
	}

	pubDateNodes, err := sub.Find("pubDate")
	if err != nil {
		t.Fatalf("sub Find pubDate: %v", err)
	}
	if len(pubDateNodes) != 1 {
		t.Fatalf("expected 1 pubDate node, got %d", len(pubDateNodes))
	}
	if pubDateNodes[0].Text() != "Mon, 01 Jan 2024 00:00:00 GMT" {
		t.Errorf("unexpected pubDate text: %q", pubDateNodes[0].Text())
	}

	// Second row sub-select
	sub2, ok := rows[1].(SubSelectable)
	if !ok {
		t.Fatal("expected row 1 node to implement SubSelectable")
	}
	title2, err := sub2.Find("title")
	if err != nil {
		t.Fatalf("sub Find title 2: %v", err)
	}
	if len(title2) != 1 {
		t.Fatalf("expected 1 title node in row 2, got %d", len(title2))
	}
	if title2[0].Text() != "Debian 12 Bookworm" {
		t.Errorf("unexpected title text row 2: %q", title2[0].Text())
	}
}

func TestXMLDocument_SelectByAttribute(t *testing.T) {
	doc, err := NewXMLDocument(testTorznabXML)
	if err != nil {
		t.Fatalf("NewXMLDocument: %v", err)
	}

	// Find items by [name=category] attribute
	rows, err := doc.Find("rss > channel > item")
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

	// Select torznab:attr elements with name="seeders"
	seeders, err := sub.Find("[name=seeders]")
	if err != nil {
		t.Fatalf("sub Find [name=seeders]: %v", err)
	}
	if len(seeders) != 1 {
		t.Fatalf("expected 1 seeders node, got %d", len(seeders))
	}

	// Verify value attribute on the matched node
	val, ok := seeders[0].Attr("value")
	if !ok || val != "10" {
		t.Errorf("unexpected seeders value: %q (ok=%v)", val, ok)
	}

	// Select [name=size]
	sizeNodes, err := sub.Find("[name=size]")
	if err != nil {
		t.Fatalf("sub Find [name=size]: %v", err)
	}
	if len(sizeNodes) != 1 {
		t.Fatalf("expected 1 size node, got %d", len(sizeNodes))
	}
	sizeVal, ok := sizeNodes[0].Attr("value")
	if !ok || sizeVal != "1024000" {
		t.Errorf("unexpected size value: %q (ok=%v)", sizeVal, ok)
	}
}

func TestXMLDocument_ExtractAttributes(t *testing.T) {
	doc, err := NewXMLDocument(testTorznabXML)
	if err != nil {
		t.Fatalf("NewXMLDocument: %v", err)
	}

	rows, err := doc.Find("rss > channel > item")
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

	enclosures, err := sub.Find("enclosure")
	if err != nil {
		t.Fatalf("sub Find enclosure: %v", err)
	}
	if len(enclosures) != 1 {
		t.Fatalf("expected 1 enclosure node, got %d", len(enclosures))
	}

	url, ok := enclosures[0].Attr("url")
	if !ok || url != "http://example.com/download/1.torrent" {
		t.Errorf("unexpected enclosure url: %q (ok=%v)", url, ok)
	}
	length, ok := enclosures[0].Attr("length")
	if !ok || length != "1024" {
		t.Errorf("unexpected enclosure length: %q (ok=%v)", length, ok)
	}
}

func TestXMLDocument_Wildcard(t *testing.T) {
	doc, err := NewXMLDocument(testTorznabXML)
	if err != nil {
		t.Fatalf("NewXMLDocument: %v", err)
	}

	rows, err := doc.Find("rss > channel > item")
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

	// Wildcard should match all direct children
	allChildren, err := sub.Find("*")
	if err != nil {
		t.Fatalf("sub Find *: %v", err)
	}
	if len(allChildren) == 0 {
		t.Fatal("expected some children with wildcard, got 0")
	}
	// item has: title, category, pubDate, link, guid, enclosure, comments, 4x torznab:attr
	if len(allChildren) < 10 {
		t.Errorf("expected at least 10 children with wildcard, got %d", len(allChildren))
	}
}

func TestXMLDocument_EmptyXML(t *testing.T) {
	_, err := NewXMLDocument("")
	if err == nil {
		t.Fatal("expected error for empty XML")
	}
}

func TestXMLDocument_InvalidXML(t *testing.T) {
	_, err := NewXMLDocument("not xml at all <><><>")
	if err == nil {
		t.Fatal("expected error for invalid XML")
	}
}

func TestXMLDocument_NoMatches(t *testing.T) {
	doc, err := NewXMLDocument(testTorznabXML)
	if err != nil {
		t.Fatalf("NewXMLDocument: %v", err)
	}

	nodes, err := doc.Find("rss > channel > nonexistent")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestXMLDocument_ChannelWithoutRSS(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<channel>
  <item><title>Test</title></item>
</channel>`
	doc, err := NewXMLDocument(xml)
	if err != nil {
		t.Fatalf("NewXMLDocument: %v", err)
	}

	nodes, err := doc.Find("channel > item")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Text() != "" {
		t.Errorf("expected empty text for item wrapper, got %q", nodes[0].Text())
	}

	sub, ok := nodes[0].(SubSelectable)
	if !ok {
		t.Fatal("expected node to implement SubSelectable")
	}
	titleNodes, err := sub.Find("title")
	if err != nil {
		t.Fatalf("sub Find title: %v", err)
	}
	if len(titleNodes) != 1 {
		t.Fatalf("expected 1 title, got %d", len(titleNodes))
	}
	if titleNodes[0].Text() != "Test" {
		t.Errorf("unexpected title: %q", titleNodes[0].Text())
	}
}

func TestXMLDocument_FindTitleFromRSS(t *testing.T) {
	doc, err := NewXMLDocument(testTorznabXML)
	if err != nil {
		t.Fatalf("NewXMLDocument: %v", err)
	}

	// Direct title from channel
	nodes, err := doc.Find("rss > channel > title")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 title node, got %d", len(nodes))
	}
	if nodes[0].Text() != "Torznab Indexer" {
		t.Errorf("unexpected title: %q", nodes[0].Text())
	}
}

func TestXMLDocument_GuidAttr(t *testing.T) {
	doc, err := NewXMLDocument(testTorznabXML)
	if err != nil {
		t.Fatalf("NewXMLDocument: %v", err)
	}

	rows, err := doc.Find("rss > channel > item")
	if err != nil {
		t.Fatalf("Find rows: %v", err)
	}

	sub, ok := rows[0].(SubSelectable)
	if !ok {
		t.Fatal("expected row to implement SubSelectable")
	}

	guidNodes, err := sub.Find("guid")
	if err != nil {
		t.Fatalf("sub Find guid: %v", err)
	}
	if len(guidNodes) != 1 {
		t.Fatalf("expected 1 guid node, got %d", len(guidNodes))
	}
	if guidNodes[0].Text() != "1" {
		t.Errorf("unexpected guid text: %q", guidNodes[0].Text())
	}
	isPermaLink, ok := guidNodes[0].Attr("isPermaLink")
	if !ok || isPermaLink != "false" {
		t.Errorf("unexpected isPermaLink: %q (ok=%v)", isPermaLink, ok)
	}
}

func TestXMLDocument_Comments(t *testing.T) {
	doc, err := NewXMLDocument(testTorznabXML)
	if err != nil {
		t.Fatalf("NewXMLDocument: %v", err)
	}

	rows, err := doc.Find("rss > channel > item")
	if err != nil {
		t.Fatalf("Find rows: %v", err)
	}

	sub, ok := rows[0].(SubSelectable)
	if !ok {
		t.Fatal("expected row to implement SubSelectable")
	}

	comments, err := sub.Find("comments")
	if err != nil {
		t.Fatalf("sub Find comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comments node, got %d", len(comments))
	}
	if !strings.HasPrefix(comments[0].Text(), "http://") {
		t.Errorf("unexpected comments: %q", comments[0].Text())
	}
}

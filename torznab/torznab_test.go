package torznab

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/Kcchouette/cardigann-go/release"
)

func TestWriteReleases_GuidNoCollisionOnSameTitle(t *testing.T) {
	// Deux reposts avec le même titre mais des liens de téléchargement
	// différents (cas fréquent sur les trackers) doivent avoir des GUID
	// distincts, sans quoi les clients Torznab (Sonarr/Radarr) qui
	// dédupliquent sur le GUID écarteraient l'un des deux résultats.
	releases := []release.Release{
		{Title: "Same.Title.2025", DownloadLink: "https://example.com/dl/1.torrent", Size: 100},
		{Title: "Same.Title.2025", DownloadLink: "https://example.com/dl/2.torrent", Size: 100},
	}

	xmlStr, err := WriteReleasesString(releases, "TestTracker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp Response
	if err := xml.Unmarshal([]byte(xmlStr), &resp); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}
	if len(resp.Channel.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Channel.Items))
	}
	if resp.Channel.Items[0].Guid == resp.Channel.Items[1].Guid {
		t.Errorf("expected distinct GUIDs for same-title releases with different download links, got %q for both", resp.Channel.Items[0].Guid)
	}
}

func TestWriteReleases_GuidPrefersInfoHash(t *testing.T) {
	releases := []release.Release{
		{Title: "Same.Title", DownloadLink: "https://example.com/dl/1.torrent", InfoHash: "hash-a"},
		{Title: "Same.Title", DownloadLink: "https://example.com/dl/1.torrent", InfoHash: "hash-b"},
	}

	xmlStr, err := WriteReleasesString(releases, "TestTracker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp Response
	if err := xml.Unmarshal([]byte(xmlStr), &resp); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}
	if resp.Channel.Items[0].Guid != "hash-a" || resp.Channel.Items[1].Guid != "hash-b" {
		t.Errorf("expected GUID to be the InfoHash, got %q and %q", resp.Channel.Items[0].Guid, resp.Channel.Items[1].Guid)
	}
}

func TestWriteReleases_ValidXML(t *testing.T) {
	releases := []release.Release{
		{
			Title:        "Ubuntu.24.04.ISO",
			DownloadLink: "https://example.com/download/1.torrent",
			Details:      "https://example.com/details/1",
			Size:         1048576,
			PublishDate:  time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
			Seeders:      42,
			Peers:        10,
			Categories:   []int{2000},
			IndexerID:    "testtracker",
			IndexerName:  "TestTracker",
		},
	}

	xmlStr, err := WriteReleasesString(releases, "TestTracker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's valid XML
	var resp Response
	if err := xml.Unmarshal([]byte(xmlStr), &resp); err != nil {
		t.Fatalf("generated XML is not valid: %v\n%s", err, xmlStr)
	}
}

func TestWriteReleases_Namespace(t *testing.T) {
	releases := []release.Release{
		{Title: "Test", DownloadLink: "https://example.com/dl.torrent", Size: 100},
	}

	xmlStr, err := WriteReleasesString(releases, "TestTracker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the correct Torznab namespace (github.io, NOT com)
	if !strings.Contains(xmlStr, "http://torznab.github.io/schemas/2015/feed") {
		t.Errorf("expected torznab.github.io namespace, got:\n%s", xmlStr)
	}
	if strings.Contains(xmlStr, "torznab.com") {
		t.Errorf("should NOT contain torznab.com, got:\n%s", xmlStr)
	}
}

func TestWriteReleases_AllFieldsMapped(t *testing.T) {
	pubDate := time.Date(2025, 6, 1, 10, 30, 0, 0, time.UTC)
	releases := []release.Release{
		{
			Title:        "Test.Release.2025",
			DownloadLink: "https://example.com/test.torrent",
			Details:      "https://example.com/details",
			InfoHash:     "abc123def456",
			Size:         2097152,
			PublishDate:  pubDate,
			Seeders:      15,
			Peers:        5,
			Grabs:        100,
			Categories:   []int{2000, 5000},
			IndexerID:    "mytracker",
			IndexerName:  "MyTracker",
			Protocol:     release.ProtocolTorrent,
		},
	}

	xmlStr, err := WriteReleasesString(releases, "MyTracker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp Response
	if err := xml.Unmarshal([]byte(xmlStr), &resp); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}

	if len(resp.Channel.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Channel.Items))
	}

	item := resp.Channel.Items[0]
	if item.Title != "Test.Release.2025" {
		t.Errorf("expected title 'Test.Release.2025', got %q", item.Title)
	}
	if item.Link != "https://example.com/test.torrent" {
		t.Errorf("expected link, got %q", item.Link)
	}
	if item.Comments != "https://example.com/details" {
		t.Errorf("expected comments, got %q", item.Comments)
	}
	if item.Size != 2097152 {
		t.Errorf("expected size 2097152, got %d", item.Size)
	}
	if item.Guid != "abc123def456" {
		t.Errorf("expected guid to be the release InfoHash, got %q", item.Guid)
	}
	if !strings.Contains(item.PubDate, "2025") {
		t.Errorf("expected pubDate to contain 2025, got %q", item.PubDate)
	}
}

func TestWriteReleases_Enclosure(t *testing.T) {
	releases := []release.Release{
		{
			Title:        "Test.Torrent",
			DownloadLink: "https://example.com/download.torrent",
			Size:         524288,
		},
	}

	xmlStr, err := WriteReleasesString(releases, "TestTracker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp Response
	if err := xml.Unmarshal([]byte(xmlStr), &resp); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}

	item := resp.Channel.Items[0]
	if item.Enclosure == nil {
		t.Fatal("expected enclosure element")
	}
	if item.Enclosure.URL != "https://example.com/download.torrent" {
		t.Errorf("expected enclosure URL, got %q", item.Enclosure.URL)
	}
	if item.Enclosure.Length != 524288 {
		t.Errorf("expected enclosure length 524288, got %d", item.Enclosure.Length)
	}
	if item.Enclosure.Type != "application/x-bittorrent" {
		t.Errorf("expected enclosure type 'application/x-bittorrent', got %q", item.Enclosure.Type)
	}
}

func TestWriteReleases_Categories(t *testing.T) {
	releases := []release.Release{
		{
			Title:      "Test.With.Categories",
			Size:       1024,
			Categories: []int{2000, 5000},
		},
	}

	xmlStr, err := WriteReleasesString(releases, "TestTracker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp Response
	if err := xml.Unmarshal([]byte(xmlStr), &resp); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}

	item := resp.Channel.Items[0]

	// Categories should appear as <category> elements
	if len(item.Categories) != 2 {
		t.Fatalf("expected 2 category elements, got %d", len(item.Categories))
	}
	if item.Categories[0] != "2000" || item.Categories[1] != "5000" {
		t.Errorf("expected categories [2000 5000], got %v", item.Categories)
	}

	// Categories should also appear as torznab:attr
	var categoryAttrs []string
	for _, attr := range item.Attrs {
		if attr.Name == "category" {
			categoryAttrs = append(categoryAttrs, attr.Value)
		}
	}
	if len(categoryAttrs) != 2 {
		t.Fatalf("expected 2 category torznab:attr, got %d", len(categoryAttrs))
	}
	if categoryAttrs[0] != "2000" || categoryAttrs[1] != "5000" {
		t.Errorf("expected category attrs [2000 5000], got %v", categoryAttrs)
	}
}

func TestWriteReleases_EmptyList(t *testing.T) {
	xmlStr, err := WriteReleasesString(nil, "EmptyTracker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp Response
	if err := xml.Unmarshal([]byte(xmlStr), &resp); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}
	if len(resp.Channel.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(resp.Channel.Items))
	}
}

func TestWriteReleases_TorznabAttrs(t *testing.T) {
	releases := []release.Release{
		{
			Title:        "Test.With.Attrs",
			DownloadLink: "https://example.com/dl.torrent",
			Size:         1024,
			Seeders:      20,
			Peers:        8,
			Grabs:        50,
			InfoHash:     "deadbeef123",
			IndexerID:    "privtracker",
		},
	}

	xmlStr, err := WriteReleasesString(releases, "TestTracker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp Response
	if err := xml.Unmarshal([]byte(xmlStr), &resp); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}

	item := resp.Channel.Items[0]
	attrs := make(map[string]string)
	for _, a := range item.Attrs {
		attrs[a.Name] = a.Value
	}

	if attrs["seeders"] != "20" {
		t.Errorf("expected seeders=20, got %q", attrs["seeders"])
	}
	if attrs["peers"] != "8" {
		t.Errorf("expected peers=8, got %q", attrs["peers"])
	}
	if attrs["grabs"] != "50" {
		t.Errorf("expected grabs=50, got %q", attrs["grabs"])
	}
	if attrs["infohash"] != "deadbeef123" {
		t.Errorf("expected infohash, got %q", attrs["infohash"])
	}
	if attrs["indexer"] != "privtracker" {
		t.Errorf("expected indexer=privtracker, got %q", attrs["indexer"])
	}
}

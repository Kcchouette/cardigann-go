package release

import (
	"testing"
	"time"
)

func TestReleaseConstruction(t *testing.T) {
	r := Release{
		Title:        "Test.Release.2026.1080p",
		Details:      "https://example.invalid/details/1",
		DownloadLink: "https://example.invalid/download/1.torrent",
		InfoHash:     "ABCDEF1234567890",
		Size:         1234567890,
		PublishDate:  time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		Seeders:      42,
		Peers:        7,
		Grabs:        100,
		Categories:   []int{2000, 2010},
		Protocol:     ProtocolTorrent,
		IndexerID:    "example",
		IndexerName:  "Example Tracker",
	}

	if r.Title != "Test.Release.2026.1080p" {
		t.Errorf("unexpected title: %q", r.Title)
	}
	if r.Size != 1234567890 {
		t.Errorf("unexpected size: %d", r.Size)
	}
	if r.Seeders != 42 {
		t.Errorf("unexpected seeders: %d", r.Seeders)
	}
	if r.Peers != 7 {
		t.Errorf("unexpected peers: %d", r.Peers)
	}
	if r.Protocol != ProtocolTorrent {
		t.Errorf("unexpected protocol: %q", r.Protocol)
	}
	if len(r.Categories) != 2 || r.Categories[0] != 2000 {
		t.Errorf("unexpected categories: %v", r.Categories)
	}
}

func TestProtocolConstants(t *testing.T) {
	if ProtocolTorrent != "torrent" {
		t.Errorf("ProtocolTorrent = %q, want %q", ProtocolTorrent, "torrent")
	}
	if ProtocolUsenet != "usenet" {
		t.Errorf("ProtocolUsenet = %q, want %q", ProtocolUsenet, "usenet")
	}
}

func TestReleaseZeroValue(t *testing.T) {
	var r Release
	if r.Title != "" {
		t.Errorf("zero-value Title should be empty, got %q", r.Title)
	}
	if r.Size != 0 {
		t.Errorf("zero-value Size should be 0, got %d", r.Size)
	}
	if r.Protocol != "" {
		t.Errorf("zero-value Protocol should be empty, got %q", r.Protocol)
	}
	if !r.PublishDate.IsZero() {
		t.Error("zero-value PublishDate should be zero")
	}
}

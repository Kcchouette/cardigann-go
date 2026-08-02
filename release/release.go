package release

import "time"

type Protocol string

const (
	ProtocolTorrent   Protocol = "torrent"
	ProtocolUsenet    Protocol = "usenet"
	ProtocolDDL       Protocol = "ddl"
	ProtocolStreaming Protocol = "streaming"
)

type Release struct {
	Title        string
	Details      string
	DownloadLink string
	InfoHash     string
	Size         int64
	PublishDate  time.Time
	Seeders      int
	Peers        int
	Grabs        int
	Categories   []int
	Protocol     Protocol
	IndexerID    string
	IndexerName  string

	// Hosts lists the file hosts of a DDL link (uptobox, 1fichier, ...),
	// empty for torrent/usenet releases.
	Hosts []string
	// Unlocked reports whether a chained download.before action (e.g. a
	// "thanks" POST) successfully resolved the release link.
	Unlocked bool
	// StreamURL is the playback URL of a streaming release.
	StreamURL string
}

package types

import "time"

type UploadIndex struct {
	UploadID  string    `json:"upload_id"`
	Timestamp time.Time `json:"timestamp"`
	Files     []string  `json:"files"`
}

type Metadata struct {
	OriginalFilename string `json:"original_filename"`
	SHA256           string `json:"sha256"`
	SHA256Strip      string `json:"sha256_strip"`
	SHA256Webp       string `json:"sha256_webp"`
	UUID             string `json:"uuid"`
}

type DirectoryListing struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

type Opponent struct {
	OpponentID string `json:"opponent_id"`
	AssetID    string `json:"asset_id"`
}

type MatchUp struct {
	Opponents []Opponent `json:"opponents"`

	Timestamp  int64 `json:"timestamp"`
	Difficulty int   `json:"difficulty"`
}

type SignedMatchUp struct {
	MatchUp MatchUp `json:"match_up"`

	Signature string `json:"signature"`
}

type Outcome struct {
	SignedMatchUp SignedMatchUp `json:"match_up"`

	WinnerID string `json:"winner_id"`

	Nonce int    `json:"nonce"`
	Hash  string `json:"hash"`

	// BrowserFingerprint *BrowserFingerprint `json:"browser_fingerprint,omitempty"`
}

type VerifiedOutcome struct {
	WinnerID  string   `json:"winner_id"`
	Opponents []string `json:"opponents"`
	Timestamp int64    `json:"timestamp"`
}

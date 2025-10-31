package matchmaker

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
}

type VerifiedOutcome struct {
	WinnerID  string   `json:"winner_id"`
	Opponents []string `json:"opponents"`
	Timestamp int64    `json:"timestamp"`
}

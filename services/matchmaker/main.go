package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/valkey-io/valkey-go"
)

const (
	Difficulty = 4
	Opponents  = 3

	TokenMaxAgeMinutes = 5
)

var (
	port       = flag.Int("port", 3000, "")
	valkeyAddr = flag.String("valkey-addr", "valkey:6379", "")

	assetsKey       = flag.String("assets-key", "metadata:assets", "")
	signatureSecret = flag.String("signature-secret", "secret", "")

	valkeyClient valkey.Client
)

type CustomContext struct {
	echo.Context
}

//nolint:tagliatelle
type Opponent struct {
	OpponentID string `json:"opponent_id"`
	AssetID    string `json:"asset_id"`
}

type MatchUp struct {
	Opponents []Opponent `json:"opponents"`

	Timestamp  int64 `json:"timestamp"`
	Difficulty int   `json:"difficulty"`
}

//nolint:tagliatelle
type SignedMatchUp struct {
	MatchUp MatchUp `json:"match_up"`

	Signature string `json:"signature"`
}

//nolint:tagliatelle
type Outcome struct {
	SignedMatchUp SignedMatchUp `json:"match_up"`

	WinnerID string `json:"winner_id"`

	Nonce int    `json:"nonce"`
	Hash  string `json:"hash"`

	// BrowserFingerprint *BrowserFingerprint `json:"browser_fingerprint,omitempty"`
}

func createMatchUp(opponents []string, signatureSecret []byte) (*SignedMatchUp, error) {
	timestamp := time.Now().Unix()

	matchUp := MatchUp{
		Opponents:  []Opponent{},
		Timestamp:  timestamp,
		Difficulty: Difficulty,
	}

	for _, assetID := range opponents {
		candidateID, err := uuid.NewRandom()
		if err != nil {
			return nil, fmt.Errorf("failed to generate candidate ID: %w", err)
		}

		matchUp.Opponents = append(matchUp.Opponents, Opponent{
			AssetID:    assetID,
			OpponentID: candidateID.String(),
		})
	}

	marshaledMatchUp, err := json.Marshal(matchUp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ballot: %w", err)
	}

	h := hmac.New(sha256.New, signatureSecret)
	h.Write(marshaledMatchUp)

	signature := hex.EncodeToString(h.Sum(nil))
	signedMatchUp := &SignedMatchUp{
		MatchUp:   matchUp,
		Signature: signature,
	}

	return signedMatchUp, nil
}

func computeHash(outcome Outcome) string {
	message := fmt.Sprintf("%s|%s|%d",
		outcome.SignedMatchUp.Signature,
		outcome.WinnerID,
		outcome.Nonce)

	h := sha256.New()
	h.Write([]byte(message))

	return hex.EncodeToString(h.Sum(nil))
}

func VerifyProof(outcome Outcome) bool {
	target := ""
	for range Difficulty {
		target += "0"
	}

	computed := computeHash(outcome)

	return computed == outcome.Hash &&
		len(outcome.Hash) >= Difficulty &&
		outcome.Hash[:Difficulty] == target
}

func GetMatchUp(c echo.Context) error {
	//nolint:forcetypeassert
	cc := c.(*CustomContext)

	ctx := cc.Request().Context()

	cmd := valkeyClient.B().Srandmember().
		Key(*assetsKey).
		Count(Opponents).
		Build()

	opponents, err := valkeyClient.Do(ctx, cmd).AsStrSlice()
	if err != nil {
		return fmt.Errorf("failed to retrieve opponents: %w", err)
	}

	matchUp, err := createMatchUp(opponents, []byte(*signatureSecret))
	if err != nil {
		return fmt.Errorf("failed to create match-up: %w", err)
	}

	//nolint:wrapcheck
	return cc.JSONPretty(http.StatusOK, matchUp, "  ")
}

//nolint:cyclop,funlen
func PostOutcome(c echo.Context) error {
	//nolint:forcetypeassert
	cc := c.(*CustomContext)

	ctx := cc.Request().Context()

	var outcome Outcome

	err := c.Bind(&outcome)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	marshaledMatchUp, err := json.Marshal(outcome.SignedMatchUp.MatchUp)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid match-up")
	}

	h := hmac.New(sha256.New, []byte(*signatureSecret))
	h.Write(marshaledMatchUp)

	signature := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(outcome.SignedMatchUp.Signature), []byte(signature)) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid match-up signature")
	}

	maxAge := time.Duration(TokenMaxAgeMinutes) * time.Minute
	if time.Since(time.Unix(outcome.SignedMatchUp.MatchUp.Timestamp, 0)) > maxAge {
		return echo.NewHTTPError(http.StatusBadRequest, "match-up expired")
	}

	if !VerifyProof(outcome) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid proof of work")
	}

	winnerID := outcome.WinnerID
	if len(winnerID) > 0 {
		validWinnder := false

		for _, candidate := range outcome.SignedMatchUp.MatchUp.Opponents {
			if winnerID == candidate.OpponentID {
				validWinnder = true

				break
			}
		}

		if !validWinnder {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid winner value")
		}
	}

	cmd := valkeyClient.B().Srandmember().
		Key(*assetsKey).
		Count(Opponents).
		Build()

	opponents, err := valkeyClient.Do(ctx, cmd).AsStrSlice()
	if err != nil {
		return fmt.Errorf("failed to retrieve opponents: %w", err)
	}

	matchUp, err := createMatchUp(opponents, []byte(*signatureSecret))
	if err != nil {
		return fmt.Errorf("failed to create match-up: %w", err)
	}

	//nolint:wrapcheck
	return cc.JSONPretty(http.StatusOK, matchUp, "  ")
}

func main() {
	flag.Parse()

	var err error

	valkeyClient, err = valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{*valkeyAddr},
	})
	if err != nil {
		panic(err)
	}

	defer valkeyClient.Close()

	e := echo.New()

	e.HideBanner = true
	e.HidePort = true

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := &CustomContext{
				Context: c,
			}

			return next(cc)
		}
	})

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${id} ${remote_ip} ${status} ${method} ${path} ${error} ${latency_human} ${bytes_in} ${bytes_out}\n",
	}))
	e.Use(middleware.Recover())

	e.GET("/match-up", GetMatchUp)
	e.POST("/outcome", PostOutcome)

	e.Logger.Fatal(e.Start(fmt.Sprintf(":%d", *port)))
}

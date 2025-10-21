package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/valkey-io/valkey-go"
)

const (
	BaseDifficulty     = 0
	Opponents          = 3
	TokenMaxAgeMinutes = 5

	BaseRequestsPerMinute = 20
	RateLimitWindow       = 60
)

var (
	port       = flag.Int("port", 3000, "")
	valkeyAddr = flag.String("valkey-addr", "valkey:6379", "")

	assetsKey    = flag.String("assets-key", "metadata:assets", "")
	ratelimitKey = flag.String("ratelimit-key", "matchmaker:ratelimit", "")
	outcomesKey  = flag.String("outcomes-key", "matchmaker:outcomes", "")

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

//nolint:tagliatelle
type VerifiedOutcome struct {
	WinnerID  string   `json:"winner_id"`
	Opponents []string `json:"opponents"`
	Timestamp int64    `json:"timestamp"`
}

func createMatchUp(opponents []string, signatureSecret []byte, difficulty int) (*SignedMatchUp, error) {
	timestamp := time.Now().Unix()

	matchUp := MatchUp{
		Opponents:  []Opponent{},
		Timestamp:  timestamp,
		Difficulty: difficulty,
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

func hashIP(ip string) string {
	h := sha256.New()
	h.Write([]byte(ip))

	return hex.EncodeToString(h.Sum(nil))
}

func publishOutcome(ctx context.Context, outcome Outcome) error {
	verifiedOutcome := VerifiedOutcome{
		WinnerID:  "",
		Opponents: []string{},
		Timestamp: outcome.SignedMatchUp.MatchUp.Timestamp,
	}

	for _, opponent := range outcome.SignedMatchUp.MatchUp.Opponents {
		if opponent.OpponentID == outcome.WinnerID {
			verifiedOutcome.WinnerID = opponent.AssetID
		} else {
			verifiedOutcome.Opponents = append(verifiedOutcome.Opponents, opponent.AssetID)
		}
	}

	jsonData, err := json.Marshal(verifiedOutcome)
	if err != nil {
		return fmt.Errorf("failed to marshal outcome data: %w", err)
	}

	cmd := valkeyClient.B().Xadd().
		Key(*outcomesKey).
		Id("*").
		FieldValue().
		FieldValue("data", string(jsonData)).
		Build()

	err = valkeyClient.Do(ctx, cmd).Error()
	if err != nil {
		return fmt.Errorf("failed to publish outcome to stream: %w", err)
	}

	return nil
}

func calculateDifficulty(ctx context.Context, hash string) (int, error) {
	key := fmt.Sprintf("%s:%s", *ratelimitKey, hash)

	now := time.Now().Unix()
	windowStart := now - RateLimitWindow

	resps := valkeyClient.DoMulti(ctx,
		valkeyClient.B().Zremrangebyscore().
			Key(key).
			Min("0").
			Max(strconv.FormatInt(windowStart, 10)).
			Build(),
		valkeyClient.B().Zcard().
			Key(key).
			Build(),
		valkeyClient.B().Zadd().
			Key(key).
			ScoreMember().
			ScoreMember(float64(now), strconv.FormatInt(now, 10)).
			Build(),
		valkeyClient.B().Expire().
			Key(key).
			Seconds(RateLimitWindow*2).
			Build(),
	)

	for _, resp := range resps {
		if resp.Error() != nil {
			return BaseDifficulty, fmt.Errorf("rate limit operation failed: %w", resp.Error())
		}
	}

	count, err := resps[1].AsInt64()
	if err != nil {
		return BaseDifficulty, fmt.Errorf("failed to parse count: %w", err)
	}

	requestsPerMinute := float64(count) / (float64(RateLimitWindow) / 60.0)
	if requestsPerMinute <= BaseRequestsPerMinute {
		return BaseDifficulty, nil
	}

	difficulty := int((requestsPerMinute - BaseRequestsPerMinute) / BaseRequestsPerMinute)

	return difficulty, nil
}

func VerifyProof(outcome Outcome) bool {
	difficulty := outcome.SignedMatchUp.MatchUp.Difficulty
	computed := computeHash(outcome)

	if difficulty == 0 {
		return computed == outcome.Hash
	}

	target := ""
	for range difficulty {
		target += "0"
	}

	return computed == outcome.Hash &&
		len(outcome.Hash) >= difficulty &&
		outcome.Hash[:difficulty] == target
}

func GetMatchUp(c echo.Context) error {
	//nolint:forcetypeassert
	cc := c.(*CustomContext)
	ctx := cc.Request().Context()
	ipHash := hashIP(cc.RealIP())

	difficulty, _ := calculateDifficulty(ctx, ipHash)

	cmd := valkeyClient.B().Srandmember().
		Key(*assetsKey).
		Count(Opponents).
		Build()

	opponents, err := valkeyClient.Do(ctx, cmd).AsStrSlice()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retrieve opponents")
	}

	if len(opponents) == 0 {
		return echo.NewHTTPError(http.StatusTooEarly, "not enough assets available")
	}

	matchUp, err := createMatchUp(opponents, []byte(*signatureSecret), difficulty)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create match-up")
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

	_ = publishOutcome(ctx, outcome)

	ipHash := hashIP(cc.RealIP())

	difficulty, _ := calculateDifficulty(ctx, ipHash)

	cmd := valkeyClient.B().Srandmember().
		Key(*assetsKey).
		Count(Opponents).
		Build()

	opponents, err := valkeyClient.Do(ctx, cmd).AsStrSlice()
	if err != nil {
		return fmt.Errorf("failed to retrieve opponents: %w", err)
	}

	if len(opponents) == 0 {
		return echo.NewHTTPError(http.StatusTooEarly, "not enough assets available")
	}

	matchUp, err := createMatchUp(opponents, []byte(*signatureSecret), difficulty)
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

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/vreid/shiki/libs/go/types"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const (
	BaseDifficulty     = 0
	Opponents          = 3
	TokenMaxAgeMinutes = 5

	BaseRequestsPerMinute = 20
	RateLimitWindow       = 60
)

func createMatchUp(opponents []string, signatureSecret []byte, difficulty int) (*types.SignedMatchUp, error) {
	timestamp := time.Now().Unix()

	matchUp := types.MatchUp{
		Opponents:  []types.Opponent{},
		Timestamp:  timestamp,
		Difficulty: difficulty,
	}

	for _, assetID := range opponents {
		candidateID, err := uuid.NewRandom()
		if err != nil {
			return nil, fmt.Errorf("failed to generate candidate ID: %w", err)
		}

		matchUp.Opponents = append(matchUp.Opponents, types.Opponent{
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
	signedMatchUp := &types.SignedMatchUp{
		MatchUp:   matchUp,
		Signature: signature,
	}

	return signedMatchUp, nil
}

func computeHash(outcome types.Outcome) string {
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

func (x *matchmaker) publishOutcome(ctx context.Context, outcome types.Outcome) error {
	verifiedOutcome := types.VerifiedOutcome{
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

	cmd := x.valkeyClient.B().Xadd().
		Key(x.outcomesKey).
		Id("*").
		FieldValue().
		FieldValue("data", string(jsonData)).
		Build()

	err = x.valkeyClient.Do(ctx, cmd).Error()
	if err != nil {
		return fmt.Errorf("failed to publish outcome to stream: %w", err)
	}

	return nil
}

func (x *matchmaker) calculateDifficulty(ctx context.Context, hash string) (int, error) {
	key := fmt.Sprintf("%s:%s", x.ratelimitKey, hash)

	now := time.Now().Unix()
	windowStart := now - RateLimitWindow

	resps := x.valkeyClient.DoMulti(ctx,
		x.valkeyClient.B().Zremrangebyscore().
			Key(key).
			Min("0").
			Max(strconv.FormatInt(windowStart, 10)).
			Build(),
		x.valkeyClient.B().Zcard().
			Key(key).
			Build(),
		x.valkeyClient.B().Zadd().
			Key(key).
			ScoreMember().
			ScoreMember(float64(now), strconv.FormatInt(now, 10)).
			Build(),
		x.valkeyClient.B().Expire().
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

func VerifyProof(outcome types.Outcome) bool {
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

func (x *matchmaker) GetMatchUp(c echo.Context) error {
	ctx := c.Request().Context()

	ipHash := hashIP(c.RealIP())

	difficulty, _ := x.calculateDifficulty(ctx, ipHash)

	cmd := x.valkeyClient.B().Srandmember().
		Key(x.assetsKey).
		Count(Opponents).
		Build()

	opponents, err := x.valkeyClient.Do(ctx, cmd).AsStrSlice()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retrieve opponents")
	}

	if len(opponents) == 0 {
		return echo.NewHTTPError(http.StatusTooEarly, "not enough assets available")
	}

	matchUp, err := createMatchUp(opponents, []byte(x.signatureSecret), difficulty)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create match-up")
	}

	//nolint:wrapcheck
	return c.JSONPretty(http.StatusOK, matchUp, "  ")
}

//nolint:cyclop,funlen
func (x *matchmaker) PostOutcome(c echo.Context) error {
	ctx := c.Request().Context()

	var outcome types.Outcome

	err := c.Bind(&outcome)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	marshaledMatchUp, err := json.Marshal(outcome.SignedMatchUp.MatchUp)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid match-up")
	}

	h := hmac.New(sha256.New, []byte(x.signatureSecret))
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

	_ = x.publishOutcome(ctx, outcome)

	ipHash := hashIP(c.RealIP())

	difficulty, _ := x.calculateDifficulty(ctx, ipHash)

	cmd := x.valkeyClient.B().Srandmember().
		Key(x.assetsKey).
		Count(Opponents).
		Build()

	opponents, err := x.valkeyClient.Do(ctx, cmd).AsStrSlice()
	if err != nil {
		return fmt.Errorf("failed to retrieve opponents: %w", err)
	}

	if len(opponents) == 0 {
		return echo.NewHTTPError(http.StatusTooEarly, "not enough assets available")
	}

	matchUp, err := createMatchUp(opponents, []byte(x.signatureSecret), difficulty)
	if err != nil {
		return fmt.Errorf("failed to create match-up: %w", err)
	}

	//nolint:wrapcheck
	return c.JSONPretty(http.StatusOK, matchUp, "  ")
}

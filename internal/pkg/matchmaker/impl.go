package matchmaker

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/samber/do/v2"
	"github.com/vreid/shiki/internal/pkg/common"
	"github.com/vreid/shiki/internal/pkg/metadata"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

var ErrNotEnoughAssets = errors.New("not enough assets available to pick opponents")

type MatchmakerService struct {
	OutcomeSink chan<- Outcome

	SignatureSecret string

	BaseDifficulty     int
	Opponents          int
	TokenMaxAgeMinutes int
}

func NewMatchmakerService(i do.Injector) (*MatchmakerService, error) {
	outcomeSink := do.MustInvokeNamed[chan<- Outcome](i, "outcome-sink")

	signatureSecret := do.MustInvokeNamed[string](i, "signature-secret")

	baseDifficulty := do.MustInvokeNamed[int](i, "base-difficulty")
	opponents := do.MustInvokeNamed[int](i, "opponents")
	tokenMaxAgeMinutes := do.MustInvokeNamed[int](i, "token-max-age-minutes")

	result := &MatchmakerService{
		OutcomeSink: outcomeSink,

		SignatureSecret: signatureSecret,

		BaseDifficulty:     baseDifficulty,
		Opponents:          opponents,
		TokenMaxAgeMinutes: tokenMaxAgeMinutes,
	}

	echoService, err := do.Invoke[*common.EchoService](i)
	if err != nil {
		return nil, fmt.Errorf("failed to create echo service: %w", err)
	}

	echoService.Register(func(e *echo.Echo) {
		apiGroup := e.Group("/api")

		matchmakerGroup := apiGroup.Group("/matchmaker")

		matchmakerGroup.GET("/match-up", result.GetMatchUp)
		matchmakerGroup.POST("/outcome", result.PostOutcome)
	})

	return result, nil
}

func CreateMatchUp(opponents []string, signatureSecret []byte, difficulty int) (*SignedMatchUp, error) {
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

func ComputeHash(outcome Outcome) string {
	message := fmt.Sprintf("%s|%s|%d",
		outcome.SignedMatchUp.Signature,
		outcome.WinnerID,
		outcome.Nonce)

	h := sha256.New()
	h.Write([]byte(message))

	return hex.EncodeToString(h.Sum(nil))
}

func PickRandomOpponents(x int) ([]string, error) {
	if x > len(metadata.Assets) {
		return nil, fmt.Errorf("%w: cannot pick %d opponents from %d assets", ErrNotEnoughAssets, x, len(metadata.Assets))
	}

	candidates := make(map[string]bool, x)

	maxIdx := big.NewInt(int64(len(metadata.Assets)))
	for len(candidates) < x {
		randIdx, err := rand.Int(rand.Reader, maxIdx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate random index: %w", err)
		}

		idx := int(randIdx.Int64())
		candidates[metadata.Assets[idx]] = true
	}

	result := make([]string, 0, x)
	for asset := range candidates {
		result = append(result, asset)
	}

	return result, nil
}

func VerifyProof(outcome Outcome) bool {
	computed := ComputeHash(outcome)

	difficulty := outcome.SignedMatchUp.MatchUp.Difficulty
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

func (s *MatchmakerService) GetMatchUp(c echo.Context) error {
	difficulty := 0

	opponents, err := PickRandomOpponents(s.Opponents)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to pick opponents")
	}

	if len(opponents) == 0 {
		return echo.NewHTTPError(http.StatusTooEarly, "not enough assets available")
	}

	matchUp, err := CreateMatchUp(opponents, []byte(s.SignatureSecret), difficulty)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create match-up")
	}

	//nolint:wrapcheck
	return c.JSONPretty(http.StatusOK, matchUp, "  ")
}

//nolint:cyclop,funlen
func (s *MatchmakerService) PostOutcome(c echo.Context) error {
	var outcome Outcome

	err := c.Bind(&outcome)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	marshaledMatchUp, err := json.Marshal(outcome.SignedMatchUp.MatchUp)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid match-up")
	}

	h := hmac.New(sha256.New, []byte(s.SignatureSecret))
	h.Write(marshaledMatchUp)

	signature := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(outcome.SignedMatchUp.Signature), []byte(signature)) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid match-up signature")
	}

	maxAge := time.Duration(s.TokenMaxAgeMinutes) * time.Minute
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

	opponents, err := PickRandomOpponents(s.Opponents)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to pick opponents")
	}

	if len(opponents) == 0 {
		return echo.NewHTTPError(http.StatusTooEarly, "not enough assets available")
	}

	if s.OutcomeSink != nil {
		s.OutcomeSink <- outcome
	}

	difficulty := 0

	matchUp, err := CreateMatchUp(opponents, []byte(s.SignatureSecret), difficulty)
	if err != nil {
		return fmt.Errorf("failed to create match-up: %w", err)
	}

	//nolint:wrapcheck
	return c.JSONPretty(http.StatusOK, matchUp, "  ")
}

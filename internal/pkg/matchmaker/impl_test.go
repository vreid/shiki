package matchmaker_test

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"

	matchmaker "github.com/vreid/shiki/internal/pkg/matchmaker"

	"github.com/google/uuid"
)

func createOutcome(matchUp *matchmaker.SignedMatchUp) (*matchmaker.Outcome, error) {
	maxIdx := big.NewInt(int64(len(matchUp.MatchUp.Opponents)))

	randIdx, err := rand.Int(rand.Reader, maxIdx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random index: %w", err)
	}

	idx := int(randIdx.Int64())

	winnerID := matchUp.MatchUp.Opponents[idx].OpponentID

	difficulty := matchUp.MatchUp.Difficulty

	targetPrefix := ""
	for range difficulty {
		targetPrefix += "0"
	}

	outcome := matchmaker.Outcome{
		SignedMatchUp: *matchUp,
		WinnerID:      winnerID,
		Nonce:         0,
		Hash:          "",
	}

	for {
		hash := matchmaker.ComputeHash(outcome)

		if difficulty == 0 || (len(hash) >= difficulty && hash[:difficulty] == targetPrefix) {
			outcome.Hash = hash

			return &outcome, nil
		}

		outcome.Nonce++
	}
}

func BenchmarkCreateMatchUpPostOutcome(b *testing.B) {
	opponents := 3
	signatureSecret := uuid.New().String()

	for b.Loop() {
		opponents, err := matchmaker.PickRandomOpponents(opponents)
		if err != nil {
			b.Error(err)
		}

		matchUp, err := matchmaker.CreateMatchUp(opponents, []byte(signatureSecret), 0)
		if err != nil {
			b.Error(err)
		}

		outcome, err := createOutcome(matchUp)
		if err != nil {
			b.Error(err)
		}

		if !matchmaker.VerifyProof(*outcome) {
			b.FailNow()
		}
	}
}

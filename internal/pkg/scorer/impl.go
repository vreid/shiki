package scorer

import (
	"errors"
	"fmt"
	"math"

	"github.com/samber/do/v2"
	"github.com/vreid/shiki/internal/pkg/common"
	"github.com/vreid/shiki/internal/pkg/matchmaker"
	"go.etcd.io/bbolt"
)

const DefaultRating = 1500.0

var (
	ErrRatingsBucketNotFound = errors.New("ratings bucket doesn't exist")
	ErrCountBucketNotFound   = errors.New("count bucket doesn't exist")
)

type ScorerService struct {
	DatabaseService *common.DatabaseService

	OutcomeSource <-chan matchmaker.Outcome
}

func NewScorerService(i do.Injector) (*ScorerService, error) {
	databaseService := do.MustInvoke[*common.DatabaseService](i)
	outcomeSource := do.MustInvokeNamed[<-chan matchmaker.Outcome](i, "outcome-source")

	result := &ScorerService{
		DatabaseService: databaseService,

		OutcomeSource: outcomeSource,
	}

	return result, nil
}

func (s *ScorerService) Start() {
	go s.processOutcomes()
}

func GetKFactor(gamesPlayed int64) float64 {
	if gamesPlayed <= 20 {
		return 128.0
	}

	if gamesPlayed <= 50 {
		return 64.0
	}

	return 32.0
}

func CalculateExpectedScore(ratingA, ratingB float64) float64 {
	return 1.0 / (1.0 + math.Pow(10, (ratingB-ratingA)/400.0))
}

func UpdateRatings(
	winnerRating float64,
	winnerCount int64,
	loserRating float64,
	loserCount int64) (float64, int64, float64, int64) {
	expectedWinner := CalculateExpectedScore(winnerRating, loserRating)

	k := (GetKFactor(winnerCount) + GetKFactor(loserCount)) / 2.0

	winnerChange := k * (1.0 - expectedWinner)
	loserChange := k * (0.0 - (1.0 - expectedWinner))

	return winnerRating + winnerChange,
		winnerCount + 1,
		loserRating + loserChange,
		loserCount + 1
}

//nolint:cyclop,funlen // Database transaction logic requires this complexity and length
func (s *ScorerService) HandleOutcome(outcome matchmaker.Outcome) {
	databaseService := s.DatabaseService

	winnerID := outcome.WinnerID
	winnerAssetID := ""

	for _, opponent := range outcome.SignedMatchUp.MatchUp.Opponents {
		if winnerID == opponent.OpponentID {
			winnerAssetID = opponent.AssetID

			break
		}
	}

	if len(winnerAssetID) == 0 {
		return
	}

	for _, opponent := range outcome.SignedMatchUp.MatchUp.Opponents {
		if winnerID == opponent.OpponentID {
			continue
		}

		_ = databaseService.DB.Update(func(tx *bbolt.Tx) error {
			ratings := tx.Bucket([]byte(common.ScorerRatingsBucket))
			if ratings == nil {
				return ErrRatingsBucketNotFound
			}

			count := tx.Bucket([]byte(common.ScorerCountBucket))
			if count == nil {
				return ErrCountBucketNotFound
			}

			loserAssetID := opponent.AssetID

			winnerRating := common.BytesToFloat64(ratings.Get([]byte(winnerAssetID)), DefaultRating)
			winnerCount := common.BytesToInt64(count.Get([]byte(winnerAssetID)), 0)

			loserRating := common.BytesToFloat64(ratings.Get([]byte(loserAssetID)), DefaultRating)
			loserCount := common.BytesToInt64(count.Get([]byte(loserAssetID)), 0)

			winnerRating, winnerCount, loserRating, loserCount =
				UpdateRatings(winnerRating, winnerCount, loserRating, loserCount)

			err := ratings.Put([]byte(winnerAssetID), common.Float64ToBytes(winnerRating))
			if err != nil {
				return fmt.Errorf("failed to put winner rating: %w", err)
			}

			err = count.Put([]byte(winnerAssetID), common.Int64ToBytes(winnerCount))
			if err != nil {
				return fmt.Errorf("failed to put winner count: %w", err)
			}

			err = ratings.Put([]byte(loserAssetID), common.Float64ToBytes(loserRating))
			if err != nil {
				return fmt.Errorf("failed to put loser rating: %w", err)
			}

			err = count.Put([]byte(loserAssetID), common.Int64ToBytes(loserCount))
			if err != nil {
				return fmt.Errorf("failed to put loser count: %w", err)
			}

			return nil
		})
	}
}

func (s *ScorerService) processOutcomes() {
	for outcome := range s.OutcomeSource {
		s.HandleOutcome(outcome)
	}
}

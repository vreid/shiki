package scorer

import (
	"math"

	"github.com/samber/do/v2"
	"github.com/vreid/shiki/internal/pkg/matchmaker"
)

type Scorecard struct {
	AssetID string
	Rating  float64
	Count   int
}

type ScorerService struct {
	OutcomeSource <-chan matchmaker.Outcome

	Ranking map[string]Scorecard
}

func NewScorerService(i do.Injector) (*ScorerService, error) {
	outcomeSource := do.MustInvokeNamed[<-chan matchmaker.Outcome](i, "outcome-source")

	result := &ScorerService{
		OutcomeSource: outcomeSource,

		Ranking: map[string]Scorecard{},
	}

	return result, nil
}

func (s *ScorerService) Start() {
	go s.processOutcomes()
}

func (s *ScorerService) processOutcomes() {
	for outcome := range s.OutcomeSource {
		s.HandleOutcome(outcome)
	}
}

func GetKFactor(gamesPlayed int) float64 {
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

func UpdateRatings(winner Scorecard, loser Scorecard) (Scorecard, Scorecard) {
	expectedWinner := CalculateExpectedScore(winner.Rating, loser.Rating)

	k := (GetKFactor(winner.Count) + GetKFactor(loser.Count)) / 2.0

	winnerChange := k * (1.0 - expectedWinner)
	loserChange := k * (0.0 - (1.0 - expectedWinner))

	updatedWinner := Scorecard{
		AssetID: winner.AssetID,
		Rating:  winner.Rating + winnerChange,
		Count:   winner.Count + 1,
	}

	updatedLoser := Scorecard{
		AssetID: loser.AssetID,
		Rating:  loser.Rating + loserChange,
		Count:   loser.Count + 1,
	}

	return updatedWinner, updatedLoser
}

func (s *ScorerService) scorecard(id string) Scorecard {
	scorecard, ok := s.Ranking[id]
	if !ok {
		scorecard = Scorecard{
			AssetID: id,
			Rating:  1500.0,
			Count:   0,
		}
	}

	return scorecard
}

func (s *ScorerService) HandleOutcome(outcome matchmaker.Outcome) {
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

		loserAssetID := opponent.AssetID

		winner := s.scorecard(winnerAssetID)
		loser := s.scorecard(loserAssetID)

		s.Ranking[winnerAssetID], s.Ranking[loserAssetID] = UpdateRatings(winner, loser)
	}
}

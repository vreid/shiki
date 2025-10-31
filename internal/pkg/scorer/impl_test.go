package scorer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vreid/shiki/internal/pkg/matchmaker"
	scorer "github.com/vreid/shiki/internal/pkg/scorer"
)

func TestCalculateExpectedScore(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0.5, scorer.CalculateExpectedScore(1500.0, 1500.0))
}

func TestUpdateRatings(t *testing.T) {
	t.Parallel()

	winner := scorer.Scorecard{
		AssetID: "1",
		Rating:  1500.0,
		Count:   100,
	}

	loser := scorer.Scorecard{
		AssetID: "1",
		Rating:  1500.0,
		Count:   100,
	}

	updatedWinner, updatedLoser := scorer.UpdateRatings(winner, loser)

	assert.Equal(t, 1516.0, updatedWinner.Rating)
	assert.Equal(t, 1484.0, updatedLoser.Rating)
}

func TestHandleOutcome(t *testing.T) {
	t.Parallel()

	scorerService := &scorer.ScorerService{
		Ranking: map[string]scorer.Scorecard{},
	}

	scorerService.HandleOutcome(matchmaker.Outcome{
		WinnerID: "o-1",
		SignedMatchUp: matchmaker.SignedMatchUp{
			MatchUp: matchmaker.MatchUp{
				Opponents: []matchmaker.Opponent{
					{
						OpponentID: "o-1",
						AssetID:    "a-1",
					},
					{
						OpponentID: "o-2",
						AssetID:    "a-2",
					},
					{
						OpponentID: "o-3",
						AssetID:    "a-3",
					},
					{
						OpponentID: "o-4",
						AssetID:    "a-4",
					},
				},
			},
		},
	})

	assert.Equal(t, 1659.67793765395, scorerService.Ranking["a-1"].Rating)
	assert.Equal(t, 1436.0, scorerService.Ranking["a-2"].Rating)
	assert.Equal(t, 1447.6576763283128, scorerService.Ranking["a-3"].Rating)
	assert.Equal(t, 1456.6643860177371, scorerService.Ranking["a-4"].Rating)
}

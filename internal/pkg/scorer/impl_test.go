package scorer_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vreid/shiki/internal/pkg/common"
	"github.com/vreid/shiki/internal/pkg/matchmaker"
	scorer "github.com/vreid/shiki/internal/pkg/scorer"
	bolt "go.etcd.io/bbolt"
)

func TestCalculateExpectedScore(t *testing.T) {
	t.Parallel()

	assert.InEpsilon(t, 0.5, scorer.CalculateExpectedScore(1500.0, 1500.0), 0.0001)
}

func TestUpdateRatings(t *testing.T) {
	t.Parallel()

	winnerRating := 1500.0
	winnerCount := int64(100)
	loserRating := 1500.0
	loserCount := int64(100)

	newWinnerRating, newWinnerCount, newLoserRating, newLoserCount :=
		scorer.UpdateRatings(winnerRating, winnerCount, loserRating, loserCount)

	assert.InEpsilon(t, 1516.0, newWinnerRating, 0.0001)
	assert.Equal(t, int64(101), newWinnerCount)
	assert.InEpsilon(t, 1484.0, newLoserRating, 0.0001)
	assert.Equal(t, int64(101), newLoserCount)
}

//nolint:funlen // Test setup requires comprehensive initialization
func TestHandleOutcome(t *testing.T) {
	t.Parallel()

	tmpPath := filepath.Join(t.TempDir(), "shiki-test.db")

	db, err := bolt.Open(tmpPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	require.NoError(t, err)

	defer func() {
		_ = db.Close()
	}()

	err = db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range []string{
			common.ScorerRatingsBucket,
			common.ScorerCountBucket,
		} {
			_, err := tx.CreateBucketIfNotExists([]byte(bucket))
			if err != nil {
				return fmt.Errorf("failed to create %s bucket: %w", bucket, err)
			}
		}

		return nil
	})
	require.NoError(t, err)

	databaseService := &common.DatabaseService{
		DB: db,
	}

	scorerService := &scorer.ScorerService{
		DatabaseService: databaseService,
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

	err = db.View(func(tx *bolt.Tx) error {
		ratings := tx.Bucket([]byte(common.ScorerRatingsBucket))

		winner := ratings.Get([]byte("a-1"))
		loser2 := ratings.Get([]byte("a-2"))
		loser3 := ratings.Get([]byte("a-3"))
		loser4 := ratings.Get([]byte("a-4"))

		assert.InEpsilon(t, 1659.0, common.BytesToFloat64(winner, 1500.0), 1.0)
		assert.InEpsilon(t, 1436.0, common.BytesToFloat64(loser2, 1500.0), 1.0)
		assert.InEpsilon(t, 1447.0, common.BytesToFloat64(loser3, 1500.0), 1.0)
		assert.InEpsilon(t, 1456.0, common.BytesToFloat64(loser4, 1500.0), 1.0)

		return nil
	})
	require.NoError(t, err)
}

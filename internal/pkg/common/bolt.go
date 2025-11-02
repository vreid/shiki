package common

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path"
	"time"

	"github.com/samber/do/v2"
	bolt "go.etcd.io/bbolt"
)

const (
	ScorerRatingsBucket = "scorer:ratings"
	ScorerCountBucket   = "scorer:count"
)

type DatabaseService struct {
	DB *bolt.DB
}

func NewDatabaseService(i do.Injector) (*DatabaseService, error) {
	dataDir := do.MustInvokeNamed[string](i, "data-dir")

	err := os.MkdirAll(dataDir, 0750)
	if err != nil {
		return nil, fmt.Errorf("failed to create database path: %w", err)
	}

	dbPath := path.Join(dataDir, "shiki.db")

	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range []string{
			ScorerRatingsBucket,
			ScorerCountBucket,
		} {
			_, err := tx.CreateBucketIfNotExists([]byte(bucket))
			if err != nil {
				return fmt.Errorf("failed to create %s bucket: %w", bucket, err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database buckets: %w", err)
	}

	return &DatabaseService{
		DB: db,
	}, nil
}

func (s *DatabaseService) Shutdown() error {
	//nolint:wrapcheck
	return s.DB.Close()
}

func Float64ToBytes(f float64) []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, math.Float64bits(f))

	return buf
}

func BytesToFloat64(b []byte, _default float64) float64 {
	if len(b) == 0 {
		return _default
	}

	bits := binary.LittleEndian.Uint64(b)

	return math.Float64frombits(bits)
}

func Int64ToBytes(i int64) []byte {
	buf := make([]byte, 8)
	//nolint:gosec // Intentional conversion for binary encoding
	binary.LittleEndian.PutUint64(buf, uint64(i))

	return buf
}

func BytesToInt64(b []byte, _default int64) int64 {
	if len(b) == 0 {
		return _default
	}

	//nolint:gosec // Intentional conversion from binary encoding
	return int64(binary.LittleEndian.Uint64(b))
}

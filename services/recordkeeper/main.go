package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
	bolt "go.etcd.io/bbolt"
)

const (
	RecordsBucket    = "records"
	MaxBatchSize     = 1000
	BatchTimeoutSecs = 1
)

var (
	valkeyAddr  = flag.String("valkey-addr", "valkey:6379", "")
	outcomesKey = flag.String("outcomes-key", "matchmaker:outcomes", "")
	dbPath      = flag.String("db-path", "/data/recordkeeper.db", "")

	consumerGroup = flag.String("consumer-group", "recordkeeper", "")
	consumerName  = flag.String("consumer-name", "recordkeeper-1", "")

	valkeyClient valkey.Client
	db           *bolt.DB
)

//nolint:tagliatelle
type VerifiedOutcome struct {
	WinnerID  string   `json:"winner_id"`
	Opponents []string `json:"opponents"`
	Timestamp int64    `json:"timestamp"`
}

func processBatch(_ context.Context, outcomes []VerifiedOutcome) error {
	err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(RecordsBucket))
		if err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}

		for _, outcome := range outcomes {
			id, err := bucket.NextSequence()
			if err != nil {
				return fmt.Errorf("failed to get next sequence: %w", err)
			}

			allAssets := append([]string{outcome.WinnerID}, outcome.Opponents...)
			value := strings.Join(allAssets, "|")

			key := fmt.Appendf(nil, "%d", id)
			err = bucket.Put(key, []byte(value))
			if err != nil {
				return fmt.Errorf("failed to write outcome %d: %w", id, err)
			}
		}

		return nil
	})

	return err
}

func consumeStream(ctx context.Context) error {
	cmd := valkeyClient.B().XgroupCreate().
		Key(*outcomesKey).
		Group(*consumerGroup).
		Id("0").
		Mkstream().
		Build()

	err := valkeyClient.Do(ctx, cmd).Error()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	lastID := ">"
	batch := make([]VerifiedOutcome, 0, MaxBatchSize)
	messageIDs := make([]string, 0, MaxBatchSize)
	batchTicker := time.NewTicker(BatchTimeoutSecs * time.Second)
	defer batchTicker.Stop()

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}

		if err := processBatch(ctx, batch); err != nil {
			log.Printf("failed to process batch of %d outcomes: %v", len(batch), err)
		} else {
			for _, msgID := range messageIDs {
				ackCmd := valkeyClient.B().Xack().
					Key(*outcomesKey).
					Group(*consumerGroup).
					Id(msgID).
					Build()
				_ = valkeyClient.Do(ctx, ackCmd).Error()
			}
			log.Printf("processed batch of %d outcomes", len(batch))
		}

		batch = batch[:0]
		messageIDs = messageIDs[:0]
	}

	for {
		select {
		case <-batchTicker.C:
			flushBatch()
			continue
		default:
		}

		cmd := valkeyClient.B().Xreadgroup().
			Group(*consumerGroup, *consumerName).
			Count(int64(MaxBatchSize)).
			Block(100).
			Streams().
			Key(*outcomesKey).
			Id(lastID).
			Build()

		resp := valkeyClient.Do(ctx, cmd)
		if resp.Error() != nil {
			errStr := resp.Error().Error()
			if errStr == "nil" || errStr == "valkey nil message" {
				continue
			}

			return fmt.Errorf("failed to read from stream: %w", resp.Error())
		}

		streams, err := resp.AsXRead()
		if err != nil {
			return fmt.Errorf("failed to parse stream response: %w", err)
		}

		for streamKey, messages := range streams {
			_ = streamKey
			for _, message := range messages {
				dataJSON, ok := message.FieldValues["data"]
				if !ok {
					log.Printf("message %s missing 'data' field", message.ID)
					continue
				}

				var outcome VerifiedOutcome

				err := json.Unmarshal([]byte(dataJSON), &outcome)
				if err != nil {
					log.Printf("failed to unmarshal outcome from message %s: %v", message.ID, err)
					continue
				}

				batch = append(batch, outcome)
				messageIDs = append(messageIDs, message.ID)

				if len(batch) >= MaxBatchSize {
					flushBatch()
				}
			}
		}
	}
}

func main() {
	flag.Parse()

	var err error

	valkeyClient, err = valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{*valkeyAddr},
	})
	if err != nil {
		log.Fatalf("failed to create valkey client: %v", err)
	}

	defer valkeyClient.Close()

	db, err = bolt.Open(*dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}

	defer db.Close()

	ctx := context.Background()

	log.Printf("starting recordkeeper service...")
	log.Printf("valkey: %s", *valkeyAddr)
	log.Printf("outcomes stream: %s", *outcomesKey)
	log.Printf("database: %s", *dbPath)

	err = consumeStream(ctx)
	if err != nil {
		log.Fatalf("stream consumer failed: %v", err)
	}
}

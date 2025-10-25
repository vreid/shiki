package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/valkey-io/valkey-go"
)

const (
	MaxBatchSize     = 1000
	BatchTimeoutSecs = 1
)

var (
	valkeyAddr  = flag.String("valkey-addr", "valkey:6379", "")
	outcomesKey = flag.String("outcomes-key", "matchmaker:outcomes", "")

	rankingKey = flag.String("ranking-key", "scorer:rankings", "")
	countKey   = flag.String("count-key", "scorer:counts", "")

	consumerGroup = flag.String("consumer-group", "scorer", "")
	consumerName  = flag.String("consumer-name", "scorer-1", "")

	valkeyClient valkey.Client

	//go:embed match-up.lua
	matchUpScript    string
	matchUpScriptSHA string
)

//nolint:tagliatelle
type VerifiedOutcome struct {
	WinnerID  string   `json:"winner_id"`
	Opponents []string `json:"opponents"`
	Timestamp int64    `json:"timestamp"`
}

func ProcessMatchUp(ctx context.Context, opponents []string) error {
	if len(opponents) == 0 {
		return fmt.Errorf("no assets provided")
	}

	args := make([]string, 0, 1+len(opponents))
	args = append(args, fmt.Sprintf("%d", len(opponents)))
	args = append(args, opponents...)

	cmd := valkeyClient.B().
		Evalsha().
		Sha1(matchUpScriptSHA).
		Numkeys(2).
		Key(*rankingKey).
		Key(*countKey).
		Arg(args...).Build()

	result := valkeyClient.Do(ctx, cmd)
	if err := result.Error(); err != nil {
		return fmt.Errorf("failed to process matchup atomically: %w", err)
	}

	return nil
}

func processBatch(ctx context.Context, outcomes []VerifiedOutcome) error {
	for _, outcome := range outcomes {
		allAssets := append([]string{outcome.WinnerID}, outcome.Opponents...)
		if err := ProcessMatchUp(ctx, allAssets); err != nil {
			return fmt.Errorf("failed to process matchup: %w", err)
		}
	}
	return nil
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

	ctx := context.Background()

	var err error

	valkeyClient, err = valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{*valkeyAddr},
	})
	if err != nil {
		panic(err)
	}

	defer valkeyClient.Close()

	cmd := valkeyClient.B().ScriptLoad().Script(matchUpScript).Build()
	result := valkeyClient.Do(ctx, cmd)
	if err := result.Error(); err != nil {
		panic(err)
	}

	matchUpScriptSHA, err = result.ToString()
	if err != nil {
		panic(err)
	}

	log.Printf("loaded matchup script with SHA: %s", matchUpScriptSHA)

	log.Printf("starting scorer service...")
	log.Printf("valkey: %s", *valkeyAddr)
	log.Printf("outcomes stream: %s", *outcomesKey)

	err = consumeStream(ctx)
	if err != nil {
		log.Fatalf("stream consumer failed: %v", err)
	}
}

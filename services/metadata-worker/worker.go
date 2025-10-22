package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/valkey-io/valkey-go"
	"go.etcd.io/bbolt"
)

var ErrBadStatus = errors.New("bad status")

//nolint:cyclop,funlen
func (x *metadataWorker) syncMetadata(ctx context.Context) error {
	db := x.db
	valkeyClient := x.valkeyClient

	log.Println("syncing to Valkey...")

	cmd := valkeyClient.B().Smembers().Key("metadata:assets").Build()

	existingMembers, err := valkeyClient.Do(ctx, cmd).AsStrSlice()
	if err != nil {
		return fmt.Errorf("failed to get existing members: %w", err)
	}

	valkeySet := make(map[string]bool)
	for _, member := range existingMembers {
		valkeySet[member] = true
	}

	dbSet := make(map[string]bool)

	err = db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("metadata"))
		if bucket == nil {
			return nil
		}

		return bucket.ForEach(func(k, _ []byte) error {
			dbSet[string(k)] = true

			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("failed to read UUIDs from database: %w", err)
	}

	var added int

	for uuid := range dbSet {
		if !valkeySet[uuid] {
			addErr := valkeyClient.Do(ctx, valkeyClient.B().Sadd().
				Key("metadata:assets").
				Member(uuid).
				Build()).Error()
			if addErr != nil {
				return fmt.Errorf("failed to add uuid to set: %w", addErr)
			}

			added++
		}
	}

	var removed int

	for uuid := range valkeySet {
		if !dbSet[uuid] {
			remErr := valkeyClient.Do(ctx, valkeyClient.B().Srem().
				Key("metadata:assets").
				Member(uuid).
				Build()).Error()
			if remErr != nil {
				return fmt.Errorf("failed to remove uuid from set: %w", remErr)
			}

			removed++
		}
	}

	log.Printf("synced metadata:assets - added: %d, removed: %d, total: %d", added, removed, len(dbSet))

	return nil
}

func (x *metadataWorker) fetchMetadata(ctx context.Context, uuid string) (*Metadata, error) {
	metadataURL := fmt.Sprintf("%s/files/%s/metadata.json", x.processorURL, uuid)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s", ErrBadStatus, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var metadata Metadata

	err = json.Unmarshal(body, &metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &metadata, nil
}

//nolint:funlen
func (x *metadataWorker) processMessage(ctx context.Context, message valkey.XRangeEntry) {
	db := x.db
	valkeyClient := x.valkeyClient
	consumerGroup := x.consumerGroup

	uuid := message.FieldValues["uuid"]

	log.Printf("processing uuid: %s", uuid)

	metadata, err := x.fetchMetadata(ctx, uuid)
	if err != nil {
		log.Printf("error fetching metadata for %s: %v", uuid, err)

		return
	}

	isNew := false

	err = db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("metadata"))

		existingData := bucket.Get([]byte(uuid))

		metadataJSON, marshalErr := json.Marshal(metadata)
		if marshalErr != nil {
			return fmt.Errorf("failed to marshal metadata: %w", marshalErr)
		}

		if existingData != nil {
			if string(existingData) == string(metadataJSON) {
				log.Printf("metadata for %s already exists and is identical, skipping", uuid)

				return nil
			}

			log.Printf("WARNING: metadata for %s already exists but differs from new metadata", uuid)
		} else {
			isNew = true
		}

		return bucket.Put([]byte(uuid), metadataJSON)
	})
	if err != nil {
		log.Printf("error storing metadata for %s: %v", uuid, err)

		return
	}

	log.Printf("stored metadata for %s", uuid)

	if isNew {
		addErr := valkeyClient.Do(ctx, valkeyClient.B().Sadd().
			Key("metadata:assets").
			Member(uuid).
			Build()).Error()
		if addErr != nil {
			log.Printf("failed to add uuid %s to set: %v", uuid, addErr)
		}

		publishErr := valkeyClient.Do(ctx, valkeyClient.B().Xadd().
			Key("metadata-ready").
			Id("*").
			FieldValue().
			FieldValue("uuid", uuid).
			Build()).Error()
		if publishErr != nil {
			log.Printf("failed to publish uuid %s to metadata-ready stream: %v", uuid, publishErr)
		} else {
			log.Printf("published uuid %s to metadata-ready stream", uuid)
		}
	}

	err = valkeyClient.Do(ctx, valkeyClient.B().Xack().
		Key("processed").
		Group(consumerGroup).
		Id(message.ID).
		Build()).Error()
	if err != nil {
		log.Printf("failed to ack message %s: %v", message.ID, err)
	}
}

//nolint:cyclop,funlen
func (x *metadataWorker) worker(ctx context.Context) error {
	db := x.db
	valkeyClient := x.valkeyClient
	consumerName := x.consumerName
	consumerGroup := x.consumerGroup

	err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("metadata"))
		if err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	err = x.syncMetadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to load metadata to Valkey: %w", err)
	}

	err = valkeyClient.Do(ctx, valkeyClient.B().XgroupCreate().
		Key("processed").
		Group(consumerGroup).
		Id("0").
		Mkstream().
		Build()).Error()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	log.Printf("listening for processed events as consumer '%s' in group '%s'", consumerName, consumerGroup)

	for {
		response := valkeyClient.Do(ctx, valkeyClient.B().Xreadgroup().
			Group(consumerGroup, consumerName).
			Block(1000).
			Streams().
			Key("processed").
			Id(">").
			Build())

		streams, err := response.AsXRead()
		if err != nil {
			msg := err.Error()
			if msg != "valkey nil message" {
				log.Printf("error reading stream: %v", err)
			}

			continue
		}

		for streamName, messages := range streams {
			log.Printf("stream: %s", streamName)

			for _, message := range messages {
				x.processMessage(ctx, message)
			}
		}
	}
}

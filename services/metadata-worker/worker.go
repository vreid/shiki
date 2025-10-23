package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/vreid/shiki/libs/go/types"

	"github.com/valkey-io/valkey-go"
)

var ErrBadStatus = errors.New("bad status")

//nolint:cyclop,funlen
func (x *metadataWorker) syncMetadata(ctx context.Context) error {
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

	metadataClient := x.metadataClient

	var metadataIDs []string

	resp, err := metadataClient.R().
		SetContext(ctx).
		SetResult(&metadataIDs).
		Get("/")
	if err != nil {
		return fmt.Errorf("failed to fetch metadata list: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("%w: %s", ErrBadStatus, resp.Status())
	}

	dbSet := make(map[string]bool)
	for _, id := range metadataIDs {
		dbSet[id] = true
	}

	var added int

	for id := range dbSet {
		if !valkeySet[id] {
			addErr := valkeyClient.Do(ctx, valkeyClient.B().Sadd().
				Key("metadata:assets").
				Member(id).
				Build()).Error()
			if addErr != nil {
				return fmt.Errorf("failed to add uuid to set: %w", addErr)
			}

			added++
		}
	}

	var removed int

	for id := range valkeySet {
		if !dbSet[id] {
			remErr := valkeyClient.Do(ctx, valkeyClient.B().Srem().
				Key("metadata:assets").
				Member(id).
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

func (x *metadataWorker) fetchMetadata(ctx context.Context, uuid string) (*types.Metadata, error) {
	client := x.processorClient

	var metadata types.Metadata

	resp, err := client.R().
		SetContext(ctx).
		SetResult(&metadata).
		Get(fmt.Sprintf("/files/%s/metadata.json", uuid))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("%w: %s", ErrBadStatus, resp.Status())
	}

	return &metadata, nil
}

//nolint:cyclop,funlen
func (x *metadataWorker) processMessage(ctx context.Context, message valkey.XRangeEntry) {
	client := x.metadataClient
	valkeyClient := x.valkeyClient
	consumerGroup := x.consumerGroup

	id := message.FieldValues["uuid"]

	log.Printf("processing id: %s", id)

	metadata, err := x.fetchMetadata(ctx, id)
	if err != nil {
		log.Printf("error fetching metadata for %s: %v", id, err)

		return
	}

	isNew := false

	var existing types.Metadata

	resp, err := client.R().
		SetContext(ctx).
		SetResult(&existing).
		Get("/" + id)
	if err != nil {
		log.Printf("error checking existing metadata for %s: %v", id, err)

		return
	}

	switch resp.StatusCode() {
	case http.StatusNotFound:
		resp, err = client.R().
			SetContext(ctx).
			SetBody(metadata).
			Post("/" + id)
		if err != nil {
			log.Printf("error creating metadata for %s: %v", id, err)

			return
		}

		if resp.IsError() {
			log.Printf("error creating metadata for %s: %s", id, resp.Status())

			return
		}

		isNew = true

		log.Printf("created metadata for %s", id)
	case http.StatusOK:
		existingJSON, err := json.Marshal(existing)
		if err != nil {
			log.Printf("error marshaling existing metadata for %s: %v", id, err)

			return
		}

		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			log.Printf("error marshaling new metadata for %s: %v", id, err)

			return
		}

		if string(existingJSON) == string(metadataJSON) {
			log.Printf("metadata for %s already exists and is identical, skipping", id)
		} else {
			log.Printf("WARNING: metadata for %s already exists but differs from new metadata", id)

			resp, err = client.R().
				SetContext(ctx).
				SetBody(metadata).
				Put("/" + id)
			if err != nil {
				log.Printf("error updating metadata for %s: %v", id, err)

				return
			}

			if resp.IsError() {
				log.Printf("error updating metadata for %s: %s", id, resp.Status())

				return
			}

			log.Printf("updated metadata for %s", id)
		}
	default:
		log.Printf("unexpected status when checking metadata for %s: %s", id, resp.Status())

		return
	}

	if isNew {
		addErr := valkeyClient.Do(ctx, valkeyClient.B().Sadd().
			Key("metadata:assets").
			Member(id).
			Build()).Error()
		if addErr != nil {
			log.Printf("failed to add uuid %s to set: %v", id, addErr)
		}

		publishErr := valkeyClient.Do(ctx, valkeyClient.B().Xadd().
			Key("metadata-ready").
			Id("*").
			FieldValue().
			FieldValue("uuid", id).
			Build()).Error()
		if publishErr != nil {
			log.Printf("failed to publish uuid %s to metadata-ready stream: %v", id, publishErr)
		} else {
			log.Printf("published uuid %s to metadata-ready stream", id)
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

func (x *metadataWorker) worker(ctx context.Context) error {
	valkeyClient := x.valkeyClient
	consumerName := x.consumerName
	consumerGroup := x.consumerGroup

	err := x.syncMetadata(ctx)
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

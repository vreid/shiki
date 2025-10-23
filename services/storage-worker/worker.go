package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/go-resty/resty/v2"
	"github.com/vreid/shiki/libs/go/types"
)

var (
	errBadStatus = errors.New("bad status")
)

func downloadFile(ctx context.Context, url, filepath string) error {
	client := resty.New()

	resp, err := client.R().
		SetContext(ctx).
		SetOutput(filepath).
		Get(url)
	if err != nil {
		return fmt.Errorf("failed to get %s: %w", url, err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("%w: %s", errBadStatus, resp.Status())
	}

	return nil
}

func listDirectory(ctx context.Context, url string) ([]types.DirectoryListing, error) {
	client := resty.New()

	var listing []types.DirectoryListing

	resp, err := client.R().
		SetContext(ctx).
		SetResult(&listing).
		Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory listing: %w", err)
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("%w: %s", errBadStatus, resp.Status())
	}

	return listing, nil
}

func deleteDirectory(ctx context.Context, url string) error {
	client := resty.New()

	resp, err := client.R().
		SetContext(ctx).
		Delete(url)
	if err != nil {
		return fmt.Errorf("failed to delete: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("%w: %s", errBadStatus, resp.Status())
	}

	return nil
}

func (x *storageWorker) storeImage(ctx context.Context, id string) error {
	dataDir := x.dataDir
	processorURL := x.processorURL

	dstDir := filepath.Join(dataDir, id)

	_, err := os.Stat(dstDir)
	if err == nil {
		log.Printf("destination directory already exists, skipping: %s", dstDir)

		return nil
	}

	err = os.MkdirAll(dstDir, 0750)
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	listURL := fmt.Sprintf("%s/%s", processorURL, id)

	listing, err := listDirectory(ctx, listURL)
	if err != nil {
		return fmt.Errorf("failed to list directory: %w", err)
	}

	for _, entry := range listing {
		if entry.IsDir {
			continue
		}

		fileURL := fmt.Sprintf("%s/files/%s/%s", processorURL, id, entry.Name)
		localPath := filepath.Join(dstDir, entry.Name)

		err = downloadFile(ctx, fileURL, localPath)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", entry.Name, err)
		}

		log.Printf("downloaded %s/%s", id, entry.Name)
	}

	deleteURL := fmt.Sprintf("%s/%s", processorURL, id)

	err = deleteDirectory(ctx, deleteURL)
	if err != nil {
		log.Printf("warning: failed to delete source directory %s: %v", id, err)
	} else {
		log.Printf("deleted source directory for %s", id)
	}

	log.Printf("stored %s in long-term storage", id)

	return nil
}

//nolint:funlen
func (x *storageWorker) worker(ctx context.Context) error {
	valkeyClient := x.valkeyClient
	consumerName := x.consumerName
	consumerGroup := x.consumerGroup

	err := valkeyClient.Do(ctx, valkeyClient.B().XgroupCreate().
		Key("metadata-ready").
		Group(consumerGroup).
		Id("0").
		Mkstream().
		Build()).Error()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	log.Printf("listening for metadata-ready events as consumer '%s' in group '%s'", consumerName, consumerGroup)

	for {
		response := valkeyClient.Do(ctx, valkeyClient.B().Xreadgroup().
			Group(consumerGroup, consumerName).
			Count(1).
			Block(1000).
			Streams().
			Key("metadata-ready").
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
				id := message.FieldValues["uuid"]

				log.Printf("storing uuid: %s", id)

				storeErr := x.storeImage(ctx, id)
				if storeErr != nil {
					log.Printf("error storing image %s: %v", id, storeErr)

					continue
				}

				ackErr := valkeyClient.Do(ctx, valkeyClient.B().Xack().
					Key("metadata-ready").
					Group(consumerGroup).
					Id(message.ID).
					Build()).Error()
				if ackErr != nil {
					log.Printf("failed to ack message %s: %v", message.ID, ackErr)
				}
			}
		}
	}
}

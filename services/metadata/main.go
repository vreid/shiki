package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/valkey-io/valkey-go"
)

var ErrBadStatus = errors.New("bad status")

var (
	imageProcessorURL = flag.String("image-processor-url", "http://traefik/processor", "")
	valkeyAddr        = flag.String("valkey-addr", "valkey:6379", "")
)

//nolint:tagliatelle
type Metadata struct {
	OriginalFilename string `json:"original_filename"`
	SHA256           string `json:"sha256"`
	SHA256Strip      string `json:"sha256_strip"`
	SHA256Webp       string `json:"sha256_webp"`
	UUID             string `json:"uuid"`
}

func fetchMetadata(ctx context.Context, uuid string) (*Metadata, error) {
	metadataURL := fmt.Sprintf("%s/%s/metadata.json", *imageProcessorURL, uuid)

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

//nolint:cyclop,funlen
func main() {
	flag.Parse()

	valkeyClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{*valkeyAddr},
	})
	if err != nil {
		panic(err)
	}
	defer valkeyClient.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	hostname, err := os.Hostname()
	if err != nil {
		log.Printf("failed to get hostname: %v", err)

		return
	}

	consumerGroup := "metadata"
	consumerName := hostname

	createGroupErr := valkeyClient.Do(ctx, valkeyClient.B().XgroupCreate().
		Key("processed").
		Group(consumerGroup).
		Id("0").
		Mkstream().
		Build()).Error()
	if createGroupErr != nil && createGroupErr.Error() != "BUSYGROUP Consumer Group name already exists" {
		log.Printf("failed to create consumer group: %v", createGroupErr)

		return
	}

	log.Printf("listening for processed events as consumer '%s' in group '%s'", consumerName, consumerGroup)

	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down gracefully...")

			return
		default:
		}

		response := valkeyClient.Do(ctx, valkeyClient.B().Xreadgroup().
			Group(consumerGroup, consumerName).
			Block(1000).
			Streams().
			Key("processed").
			Id(">").
			Build())

		if ctx.Err() != nil {
			log.Println("shutting down gracefully...")

			return
		}

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
				uuid := message.FieldValues["uuid"]

				log.Printf("processing uuid: %s", uuid)

				metadata, fetchErr := fetchMetadata(ctx, uuid)
				if fetchErr != nil {
					log.Printf("error fetching metadata for %s: %v", uuid, fetchErr)

					continue
				}

				//nolint:errchkjson
				metadataJSON, _ := json.MarshalIndent(metadata, "", "  ")
				log.Printf("metadata for %s:\n%s", uuid, string(metadataJSON))

				ackErr := valkeyClient.Do(ctx, valkeyClient.B().Xack().
					Key("processed").
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

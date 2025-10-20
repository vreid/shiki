package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/valkey-io/valkey-go"
)

var (
	processorURL = flag.String("processor-url", "http://traefik/processor", "")
	valkeyAddr   = flag.String("valkey-addr", "valkey:6379", "")
	dataDir      = flag.String("data-dir", "/data", "")
)

//nolint:tagliatelle
type DirectoryListing struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

func downloadFile(ctx context.Context, url, filepath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get %s: %w", url, err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	//nolint:gosec
	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	defer func() {
		_ = out.Close()
	}()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	return nil
}

func listDirectory(ctx context.Context, url string) ([]DirectoryListing, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory listing: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	var listing []DirectoryListing
	err = json.NewDecoder(resp.Body).Decode(&listing)
	if err != nil {
		return nil, fmt.Errorf("failed to decode listing: %w", err)
	}

	return listing, nil
}

func deleteDirectory(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	return nil
}

func storeImage(ctx context.Context, uuid string) error {
	dstDir := filepath.Join(*dataDir, uuid)

	if _, err := os.Stat(dstDir); err == nil {
		log.Printf("destination directory already exists, skipping: %s", dstDir)
		return nil
	}

	err := os.MkdirAll(dstDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	listURL := fmt.Sprintf("%s/list/%s", *processorURL, uuid)
	listing, err := listDirectory(ctx, listURL)
	if err != nil {
		return fmt.Errorf("failed to list directory: %w", err)
	}

	for _, entry := range listing {
		if entry.IsDir {
			continue
		}

		fileURL := fmt.Sprintf("%s/files/%s/%s", *processorURL, uuid, entry.Name)
		localPath := filepath.Join(dstDir, entry.Name)

		err = downloadFile(ctx, fileURL, localPath)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", entry.Name, err)
		}

		log.Printf("downloaded %s/%s", uuid, entry.Name)
	}

	deleteURL := fmt.Sprintf("%s/delete/%s", *processorURL, uuid)
	err = deleteDirectory(ctx, deleteURL)
	if err != nil {
		log.Printf("warning: failed to delete source directory %s: %v", uuid, err)
	} else {
		log.Printf("deleted source directory for %s", uuid)
	}

	log.Printf("stored %s in long-term storage", uuid)

	return nil
}

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

	consumerGroup := "storage"
	consumerName := hostname

	createGroupErr := valkeyClient.Do(ctx, valkeyClient.B().XgroupCreate().
		Key("metadata-ready").
		Group(consumerGroup).
		Id("0").
		Mkstream().
		Build()).Error()
	if createGroupErr != nil && createGroupErr.Error() != "BUSYGROUP Consumer Group name already exists" {
		log.Printf("failed to create consumer group: %v", createGroupErr)
		return
	}

	log.Printf("listening for metadata-ready events as consumer '%s' in group '%s'", consumerName, consumerGroup)

	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down gracefully...")
			return
		default:
		}

		response := valkeyClient.Do(ctx, valkeyClient.B().Xreadgroup().
			Group(consumerGroup, consumerName).
			Count(1).
			Block(1000).
			Streams().
			Key("metadata-ready").
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

				log.Printf("storing uuid: %s", uuid)

				storeErr := storeImage(ctx, uuid)
				if storeErr != nil {
					log.Printf("error storing image %s: %v", uuid, storeErr)
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

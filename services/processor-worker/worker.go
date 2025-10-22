package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/valkey-io/valkey-go"
)

var ErrScriptFailed = errors.New("script execution failed")

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
		return fmt.Errorf("bad status: %s: %w", resp.Status, err)
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

//nolint:cyclop,funlen
func (x *processorWorker) processUpload(ctx context.Context, uploadID string) error {
	indexURL := fmt.Sprintf("%s/files/%s/index.json", x.receiverURL, uploadID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch index.json: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch index.json: status %d: %w", resp.StatusCode, err)
	}

	var index UploadIndex

	err = json.NewDecoder(resp.Body).Decode(&index)
	if err != nil {
		return fmt.Errorf("failed to decode index.json: %w", err)
	}

	log.Printf("processing %d files for upload %s", len(index.Files), uploadID)

	tempDir, err := os.MkdirTemp("", fmt.Sprintf("upload-%s-*", uploadID))
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	for _, filename := range index.Files {
		fileURL := fmt.Sprintf("%s/files/%s/%s", x.receiverURL, uploadID, filename)
		localPath := filepath.Join(tempDir, filename)

		err = downloadFile(ctx, fileURL, localPath)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", filename, err)
		}

		log.Printf("downloaded: %s", filename)

		scriptPath := filepath.Join(x.scriptDir, "process-image.sh")

		//nolint:gosec,noctx
		cmd := exec.Command(scriptPath, localPath, x.dataDir)
		cmd.Dir = x.dataDir

		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to execute script for %s: %w", filename, err)
		}

		var result ScriptResult

		err = json.Unmarshal(output, &result)
		if err != nil {
			return fmt.Errorf("failed to parse script output for %s: %w (output: %s)", filename, err, string(output))
		}

		if result.Error != "" {
			return fmt.Errorf("%w for %s: %s", ErrScriptFailed, filename, result.Error)
		}

		log.Printf("processed %s: uuid=%s", filename, result.Success)

		valkeyClient := x.valkeyClient

		publishErr := valkeyClient.Do(ctx, valkeyClient.B().Xadd().
			Key("processed").
			Id("*").
			FieldValue().
			FieldValue("uuid", result.Success).
			Build()).Error()
		if publishErr != nil {
			return fmt.Errorf("failed to publish processed image %s: %w", result.Success, publishErr)
		}
	}

	return nil
}

func (x *processorWorker) processMessage(ctx context.Context, message valkey.XRangeEntry) {
	uploadID := message.FieldValues["upload_id"]

	log.Printf("processing upload: id=%s", uploadID)

	err := x.processUpload(ctx, uploadID)
	if err != nil {
		log.Printf("error processing upload %s: %v", uploadID, err)

		return
	}

	deleteURL := fmt.Sprintf("%s/%s", x.receiverURL, uploadID)

	deleteRequest, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		log.Printf("failed to create delete request for %s: %v", uploadID, err)

		return
	}

	deleteResponse, err := http.DefaultClient.Do(deleteRequest)
	if err != nil {
		log.Printf("failed to delete upload %s: %v", uploadID, err)

		return
	}

	_ = deleteResponse.Body.Close()
	if deleteResponse.StatusCode == http.StatusNoContent {
		log.Printf("deleted upload %s from receiver", uploadID)
	} else {
		log.Printf("failed to delete upload %s: status %d", uploadID, deleteResponse.StatusCode)
	}

	valkeyClient := x.valkeyClient

	err = valkeyClient.Do(ctx, valkeyClient.B().Xack().
		Key("uploads").
		Group(x.consumerGroup).
		Id(message.ID).
		Build()).Error()
	if err != nil {
		log.Printf("failed to ack message %s: %v", message.ID, err)
	}
}

func (x *processorWorker) worker(ctx context.Context) error {
	valkeyClient := x.valkeyClient
	consumerName := x.consumerName
	consumerGroup := x.consumerGroup

	defer valkeyClient.Close()

	err := valkeyClient.Do(ctx, valkeyClient.B().XgroupCreate().
		Key("uploads").
		Group(consumerGroup).
		Id("0").
		Mkstream().
		Build()).Error()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	log.Printf("listening for upload events as consumer '%s' in group '%s'", consumerName, consumerGroup)

	for {
		response := valkeyClient.Do(ctx, valkeyClient.B().Xreadgroup().
			Group(consumerGroup, consumerName).
			Count(1).
			Block(1000).
			Streams().
			Key("uploads").
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

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
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/valkey-io/valkey-go"
)

var (
	port        = flag.Int("port", 3000, "")
	dataDir     = flag.String("data-dir", "/data", "")
	scriptDir   = flag.String("script-dir", "/app/tools", "")
	receiverURL = flag.String("receiver-url", "http://traefik/receiver", "")
	valkeyAddr  = flag.String("valkey-addr", "valkey:6379", "")

	valkeyClient valkey.Client
)

//nolint:tagliatelle
type UploadIndex struct {
	UploadID  string    `json:"upload_id"`
	Timestamp time.Time `json:"timestamp"`
	Files     []string  `json:"files"`
}

type ScriptResult struct {
	Success string `json:"success"`
	Error   string `json:"error"`
}

var ErrScriptFailed = errors.New("script execution failed")

//nolint:tagliatelle
type DirectoryEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

func listDirectory(c echo.Context) error {
	uuid := c.Param("uuid")
	if uuid == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "uuid is required")
	}

	dirPath := filepath.Join(*dataDir, uuid)

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "directory not found")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read directory")
	}

	result := make([]DirectoryEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		result = append(result, DirectoryEntry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Size:  info.Size(),
		})
	}

	//nolint:wrapcheck
	return c.JSON(http.StatusOK, result)
}

func deleteDirectory(c echo.Context) error {
	uuid := c.Param("uuid")
	if uuid == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "uuid is required")
	}

	dirPath := filepath.Join(*dataDir, uuid)

	err := os.RemoveAll(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "directory not found")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete directory")
	}

	return c.NoContent(http.StatusNoContent)
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
func processUpload(ctx context.Context, uploadID string) error {
	indexURL := fmt.Sprintf("%s/files/%s/index.json", *receiverURL, uploadID)

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
		fileURL := fmt.Sprintf("%s/files/%s/%s", *receiverURL, uploadID, filename)
		localPath := filepath.Join(tempDir, filename)

		err = downloadFile(ctx, fileURL, localPath)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", filename, err)
		}

		log.Printf("downloaded: %s", filename)

		scriptPath := filepath.Join(*scriptDir, "process-image.sh")

		//nolint:gosec,noctx
		cmd := exec.Command(scriptPath, localPath, *dataDir)
		cmd.Dir = *dataDir

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

//nolint:cyclop,funlen,gocognit
func main() {
	flag.Parse()

	var err error

	valkeyClient, err = valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{*valkeyAddr},
	})
	if err != nil {
		panic(err)
	}
	defer valkeyClient.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	e := echo.New()

	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${id} ${remote_ip} ${status} ${method} ${path} ${error} ${latency_human} ${bytes_in} ${bytes_out}\n",
	}))
	e.Use(middleware.Recover())

	e.GET("/:uuid", listDirectory)
	e.DELETE("/:uuid", deleteDirectory)

	e.Static("/files", *dataDir)

	go func() {
		startErr := e.Start(fmt.Sprintf(":%d", *port))
		if startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
			log.Fatalf("failed to start server: %v", startErr)
		}
	}()

	go func() {
		<-ctx.Done()
		log.Println("shutting down HTTP server...")

		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
		defer shutdownCancel()

		shutdownErr := e.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			log.Printf("error shutting down server: %v", shutdownErr)
		}
	}()

	hostname, err := os.Hostname()
	if err != nil {
		log.Printf("failed to get hostname: %v", err)

		return
	}

	consumerGroup := "processors"
	consumerName := hostname

	createGroupErr := valkeyClient.Do(ctx, valkeyClient.B().XgroupCreate().
		Key("uploads").
		Group(consumerGroup).
		Id("0").
		Mkstream().
		Build()).Error()
	if createGroupErr != nil && createGroupErr.Error() != "BUSYGROUP Consumer Group name already exists" {
		log.Printf("failed to create consumer group: %v", createGroupErr)

		return
	}

	log.Printf("listening for upload events as consumer '%s' in group '%s'", consumerName, consumerGroup)

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
			Key("uploads").
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
				uploadID := message.FieldValues["upload_id"]

				log.Printf("processing upload: id=%s", uploadID)

				processErr := processUpload(ctx, uploadID)
				if processErr != nil {
					log.Printf("error processing upload %s: %v", uploadID, processErr)

					continue
				}

				deleteURL := fmt.Sprintf("%s/%s", *receiverURL, uploadID)
				deleteReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
				if err != nil {
					log.Printf("failed to create delete request for %s: %v", uploadID, err)
				} else {
					deleteResp, err := http.DefaultClient.Do(deleteReq)
					if err != nil {
						log.Printf("failed to delete upload %s: %v", uploadID, err)
					} else {
						_ = deleteResp.Body.Close()
						if deleteResp.StatusCode == http.StatusNoContent {
							log.Printf("deleted upload %s from receiver", uploadID)
						} else {
							log.Printf("failed to delete upload %s: status %d", uploadID, deleteResp.StatusCode)
						}
					}
				}

				ackErr := valkeyClient.Do(ctx, valkeyClient.B().Xack().
					Key("uploads").
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

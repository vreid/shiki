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
	"path/filepath"
	"syscall"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/valkey-io/valkey-go"
	"go.etcd.io/bbolt"
)

var ErrBadStatus = errors.New("bad status")

var (
	processorURL = flag.String("processor-url", "http://traefik/processor", "")
	valkeyAddr   = flag.String("valkey-addr", "valkey:6379", "")
	dataDir      = flag.String("data-dir", "/data", "")
	port         = flag.Int("port", 3000, "")
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
	metadataURL := fmt.Sprintf("%s/files/%s/metadata.json", *processorURL, uuid)

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

func list(db *bbolt.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		var uuids []string

		err := db.View(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket([]byte("metadata"))
			if bucket == nil {
				return nil
			}

			return bucket.ForEach(func(k, _ []byte) error {
				uuids = append(uuids, string(k))

				return nil
			})
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to list UUIDs")
		}

		return c.JSONPretty(http.StatusOK, uuids, "  ")
	}
}

//nolint:cyclop,funlen,gocognit
func main() {
	flag.Parse()

	dbPath := filepath.Join(*dataDir, "metadata.db")

	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}

	defer func() {
		_ = db.Close()
	}()

	err = db.Update(func(tx *bbolt.Tx) error {
		_, createErr := tx.CreateBucketIfNotExists([]byte("metadata"))
		if createErr != nil {
			return fmt.Errorf("failed to create bucket: %w", createErr)
		}

		return nil
	})
	if err != nil {
		log.Printf("failed to create bucket: %v", err)

		return
	}

	valkeyClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{*valkeyAddr},
	})
	if err != nil {
		panic(err)
	}
	defer valkeyClient.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Println("clearing and populating UUIDs set in Valkey...")
	err = valkeyClient.Do(ctx, valkeyClient.B().Del().Key("metadata:assets").Build()).Error()
	if err != nil {
		log.Printf("failed to clear UUIDs set: %v", err)
		return
	}

	var uuidCount int
	err = db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("metadata"))
		if bucket == nil {
			return nil
		}

		return bucket.ForEach(func(k, _ []byte) error {
			addErr := valkeyClient.Do(ctx, valkeyClient.B().Sadd().
				Key("metadata:assets").
				Member(string(k)).
				Build()).Error()
			if addErr != nil {
				return fmt.Errorf("failed to add uuid to set: %w", addErr)
			}
			uuidCount++
			return nil
		})
	})
	if err != nil {
		log.Printf("failed to populate UUIDs set: %v", err)
		return
	}
	log.Printf("populated %d UUIDs into Valkey set", uuidCount)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${id} ${remote_ip} ${status} ${method} ${path} ${error} ${latency_human} ${bytes_in} ${bytes_out}\n",
	}))
	e.Use(middleware.Recover())

	e.GET("/", list(db))

	go func() {
		log.Printf("starting HTTP server on :%d", *port)

		startErr := e.Start(fmt.Sprintf(":%d", *port))
		if startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", startErr)
		}
	}()

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

				isNew := false

				storeErr := db.Update(func(tx *bbolt.Tx) error {
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
				if storeErr != nil {
					log.Printf("error storing metadata for %s: %v", uuid, storeErr)

					continue
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

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
	"github.com/valkey-io/valkey-go"
	"go.etcd.io/bbolt"
)

type metadataWorker struct {
	db           *bbolt.DB
	valkeyClient valkey.Client

	dataDir      string
	processorURL string

	consumerGroup string
	consumerName  string
}

func newMetadataWorker(cmd *cli.Command) (*metadataWorker, error) {
	valkeyAddr := cmd.String("valkey-addr")
	dataDir := cmd.String("data-dir")
	processorURL := cmd.String("processor-url")

	dbPath := filepath.Join(dataDir, "metadata.db")

	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}

	valkeyClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{valkeyAddr},
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't create valkey client: %w", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	return &metadataWorker{
		db:           db,
		valkeyClient: valkeyClient,

		dataDir:      dataDir,
		processorURL: processorURL,

		consumerGroup: "metadata",
		consumerName:  hostname,
	}, nil
}

func runWorker(ctx context.Context, cmd *cli.Command) error {
	metadataWorker, err := newMetadataWorker(cmd)
	if err != nil {
		return fmt.Errorf("couldn't create metadata worker: %w", err)
	}

	return metadataWorker.worker(ctx)
}

func main() {
	//nolint:exhaustruct
	cmd := &cli.Command{
		Name: "metadata-worker",
		Commands: []*cli.Command{
			{
				Name: "worker",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "data-dir",
						Value:   "/data",
						Sources: cli.EnvVars("METADATA_WORKER_DATA_DIR"),
					},
					&cli.StringFlag{
						Name:    "processor-url",
						Value:   "http://traefik/processor",
						Sources: cli.EnvVars("METADATA_WORKER_PROCESSOR_URL"),
					},
					&cli.StringFlag{
						Name:    "valkey-addr",
						Value:   "valkey:6379",
						Sources: cli.EnvVars("METADATA_WORKER_VALKEY_ADDR"),
					},
				},
				Action: runWorker,
			},
		},
		DefaultCommand: "worker",
	}

	err := cmd.Run(context.Background(), os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

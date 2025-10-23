package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli/v3"
	"github.com/valkey-io/valkey-go"
)

type storageWorker struct {
	valkeyClient valkey.Client

	dataDir string

	processorURL string
	storageURL   string

	consumerGroup string
	consumerName  string
}

func newStorageWorker(cmd *cli.Command) (*storageWorker, error) {
	valkeyAddr := cmd.String("valkey-addr")

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

	return &storageWorker{
		valkeyClient: valkeyClient,

		dataDir: cmd.String("data-dir"),

		processorURL: cmd.String("processor-url"),
		storageURL:   cmd.String("storage-url"),

		consumerGroup: "storage",
		consumerName:  hostname,
	}, nil
}

func runWorker(ctx context.Context, cmd *cli.Command) error {
	storageWorker, err := newStorageWorker(cmd)
	if err != nil {
		return fmt.Errorf("couldn't create storage worker: %w", err)
	}

	return storageWorker.worker(ctx)
}

func main() {
	//nolint:exhaustruct
	cmd := &cli.Command{
		Name: "storage-worker",
		Commands: []*cli.Command{
			{
				Name: "worker",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "data-dir",
						Value:   "/data",
						Sources: cli.EnvVars("STORAGE_WORKER_DATA_DIR"),
					},
					&cli.StringFlag{
						Name:    "processor-url",
						Value:   "http://traefik/processor",
						Sources: cli.EnvVars("STORAGE_WORKER_PROCESSOR_URL"),
					},
					&cli.StringFlag{
						Name:    "storage-url",
						Value:   "http://traefik/storage",
						Sources: cli.EnvVars("STORAGE_WORKER_STORAGE_URL"),
					},
					&cli.StringFlag{
						Name:    "valkey-addr",
						Value:   "valkey:6379",
						Sources: cli.EnvVars("STORAGE_WORKER_VALKEY_ADDR"),
					},
				},
				Action: runWorker,
			},
		},
		DefaultCommand: "server",
	}

	err := cmd.Run(context.Background(), os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

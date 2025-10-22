package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli/v3"
	"github.com/valkey-io/valkey-go"
)

type processorWorker struct {
	valkeyClient valkey.Client

	dataDir     string
	scriptDir   string
	receiverURL string

	consumerGroup string
	consumerName  string
}

func newProcessorWorker(cmd *cli.Command) (*processorWorker, error) {
	valkeyAddr := cmd.String("valkey-addr")
	dataDir := cmd.String("data-dir")
	scriptDir := cmd.String("script-dir")
	receiverURL := cmd.String("receiver-url")

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

	return &processorWorker{
		valkeyClient: valkeyClient,

		dataDir:       dataDir,
		scriptDir:     scriptDir,
		receiverURL:   receiverURL,
		consumerGroup: "processors",
		consumerName:  hostname,
	}, nil
}

func runWorker(ctx context.Context, cmd *cli.Command) error {
	processorWorker, err := newProcessorWorker(cmd)
	if err != nil {
		return fmt.Errorf("couldn't create processor worker: %w", err)
	}

	return processorWorker.worker(ctx)
}

func main() {
	//nolint:exhaustruct
	cmd := &cli.Command{
		Name: "processor-worker",
		Commands: []*cli.Command{
			{
				Name: "worker",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "data-dir",
						Value:   "/data",
						Sources: cli.EnvVars("PROCESSOR_WORKER_DATA_DIR"),
					},
					&cli.StringFlag{
						Name:    "script-dir",
						Value:   "/app/tools",
						Sources: cli.EnvVars("PROCESSOR_WORKER_SCRIPT_DIR"),
					},
					&cli.StringFlag{
						Name:    "receiver-url",
						Value:   "http://traefik/receiver",
						Sources: cli.EnvVars("PROCESSOR_WORKER_RECEIVER_URL"),
					},
					&cli.StringFlag{
						Name:    "valkey-addr",
						Value:   "valkey:6379",
						Sources: cli.EnvVars("PROCESSOR_WORKER_VALKEY_ADDR"),
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

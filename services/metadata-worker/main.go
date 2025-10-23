package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-resty/resty/v2"
	"github.com/urfave/cli/v3"
	"github.com/valkey-io/valkey-go"
)

type metadataWorker struct {
	valkeyClient valkey.Client

	metadataClient  *resty.Client
	processorClient *resty.Client

	consumerGroup string
	consumerName  string
}

func newMetadataWorker(cmd *cli.Command) (*metadataWorker, error) {
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

	metadataClient := resty.New().
		SetBaseURL(cmd.String("metadata-url"))

	processorClient := resty.New().
		SetBaseURL(cmd.String("processor-url"))

	return &metadataWorker{
		valkeyClient: valkeyClient,

		metadataClient:  metadataClient,
		processorClient: processorClient,

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
						Name:    "metadata-url",
						Value:   "http://traefik/metadata",
						Sources: cli.EnvVars("METADATA_WORKER_METADATA_URL"),
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

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/urfave/cli/v3"
	"github.com/valkey-io/valkey-go"
)

type receiver struct {
	valkeyClient valkey.Client

	dataDir string
}

func newReceiver(cmd *cli.Command) (*receiver, error) {
	valkeyAddr := cmd.String("valkey-addr")
	dataDir := cmd.String("data-dir")

	valkeyClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{valkeyAddr},
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't create valkey client: %w", err)
	}

	return &receiver{
		valkeyClient: valkeyClient,
		dataDir:      dataDir,
	}, nil
}

func runServer(_ context.Context, cmd *cli.Command) error {
	port := cmd.Int("port")

	receiver, err := newReceiver(cmd)
	if err != nil {
		return fmt.Errorf("couldn't create receiver service: %w", err)
	}

	defer receiver.valkeyClient.Close()

	e := echo.New()

	e.HideBanner = true
	e.HidePort = false

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${id} ${remote_ip} ${status} ${method} ${path} ${error} ${latency_human} ${bytes_in} ${bytes_out}\n",
	}))
	e.Use(middleware.Recover())

	e.POST("/upload", receiver.upload)
	e.GET("/", receiver.list)
	e.DELETE("/:id", receiver.deleteUpload)

	e.Static("/files", receiver.dataDir)

	//nolint:wrapcheck
	return e.Start(fmt.Sprintf(":%d", port))
}

func main() {
	//nolint:exhaustruct
	cmd := &cli.Command{
		Name: "receiver",
		Commands: []*cli.Command{
			{
				Name: "server",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "port",
						Value:   3000, //nolint:mnd
						Sources: cli.EnvVars("RECEIVER_PORT"),
					},
					&cli.StringFlag{
						Name:    "data-dir",
						Value:   "/data",
						Sources: cli.EnvVars("RECEIVER_DATA_DIR"),
					},
					&cli.StringFlag{
						Name:    "valkey-addr",
						Value:   "valkey:6379",
						Sources: cli.EnvVars("RECEIVER_VALKEY_ADDR"),
					},
				},
				Action: runServer,
			},
		},
		DefaultCommand: "server",
	}

	err := cmd.Run(context.Background(), os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

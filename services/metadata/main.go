package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/urfave/cli/v3"
	"go.etcd.io/bbolt"
)

type metadata struct {
	db *bbolt.DB

	dataDir string
}

func newMetadata(cmd *cli.Command) (*metadata, error) {
	dataDir := cmd.String("data-dir")

	dbPath := filepath.Join(dataDir, "metadata.db")

	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{ReadOnly: true})
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}

	return &metadata{
		db: db,

		dataDir: dataDir,
	}, nil
}

func runServer(_ context.Context, cmd *cli.Command) error {
	port := cmd.Int("port")

	metadata, err := newMetadata(cmd)
	if err != nil {
		return fmt.Errorf("couldn't create metadata service: %w", err)
	}

	e := echo.New()

	e.HideBanner = true
	e.HidePort = false

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${id} ${remote_ip} ${status} ${method} ${path} ${error} ${latency_human} ${bytes_in} ${bytes_out}\n",
	}))
	e.Use(middleware.Recover())

	e.GET("/", metadata.list)

	//nolint:wrapcheck
	return e.Start(fmt.Sprintf(":%d", port))
}

func main() {
	//nolint:exhaustruct
	cmd := &cli.Command{
		Name: "metadata",
		Commands: []*cli.Command{
			{
				Name: "server",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "port",
						Value:   3000, //nolint:mnd
						Sources: cli.EnvVars("METADATA_PORT"),
					},
					&cli.StringFlag{
						Name:    "data-dir",
						Value:   "/data",
						Sources: cli.EnvVars("METADATA_DATA_DIR"),
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

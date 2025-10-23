package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type storage struct {
	dataDir string
}

func newStorage(cmd *cli.Command) *storage {
	return &storage{
		dataDir: cmd.String("data-dir"),
	}
}

func runServer(_ context.Context, cmd *cli.Command) error {
	port := cmd.Int("port")

	storage := newStorage(cmd)

	e := echo.New()

	e.HideBanner = true
	e.HidePort = false

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${id} ${remote_ip} ${status} ${method} ${path} ${error} ${latency_human} ${bytes_in} ${bytes_out}\n",
	}))
	e.Use(middleware.Recover())

	e.GET("/image/:id/:size", storage.serveImage)

	//nolint:wrapcheck
	return e.Start(fmt.Sprintf(":%d", port))
}

func main() {
	//nolint:exhaustruct
	cmd := &cli.Command{
		Name: "storage",
		Commands: []*cli.Command{
			{
				Name: "server",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "port",
						Value:   3000, //nolint:mnd
						Sources: cli.EnvVars("STORAGE_PORT"),
					},
					&cli.StringFlag{
						Name:    "data-dir",
						Value:   "/data",
						Sources: cli.EnvVars("STORAGE_DATA_DIR"),
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

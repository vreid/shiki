package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/urfave/cli/v3"
)

type processor struct {
	dataDir string
}

func newProcessor(cmd *cli.Command) *processor {
	dataDir := cmd.String("data-dir")

	return &processor{
		dataDir: dataDir,
	}
}

func runServer(_ context.Context, cmd *cli.Command) error {
	port := cmd.Int("port")

	processor := newProcessor(cmd)

	e := echo.New()

	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${id} ${remote_ip} ${status} ${method} ${path} ${error} ${latency_human} ${bytes_in} ${bytes_out}\n",
	}))
	e.Use(middleware.Recover())

	e.GET("/:uuid", processor.listDirectory)
	e.DELETE("/:uuid", processor.deleteDirectory)

	e.Static("/files", processor.dataDir)

	//nolint:wrapcheck
	return e.Start(fmt.Sprintf(":%d", port))
}

func main() {
	//nolint:exhaustruct
	cmd := &cli.Command{
		Name: "processor",
		Commands: []*cli.Command{
			{
				Name: "serve",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "data-dir",
						Value:   "/data",
						Sources: cli.EnvVars("PROCESSOR_WORKER_DATA_DIR"),
					},
				},
				Action: runServer,
			},
		},
		DefaultCommand: "serve",
	}

	err := cmd.Run(context.Background(), os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

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

type matchmaker struct {
	valkeyClient valkey.Client

	assetsKey    string
	ratelimitKey string
	outcomesKey  string

	signatureSecret string
}

func newMatchmaker(cmd *cli.Command) (*matchmaker, error) {
	valkeyAddr := cmd.String("valkey-addr")

	valkeyClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{valkeyAddr},
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't create valkey client: %w", err)
	}

	return &matchmaker{
		valkeyClient: valkeyClient,

		assetsKey:    cmd.String("assets-key"),
		ratelimitKey: cmd.String("ratelimit-key"),
		outcomesKey:  cmd.String("outcomes-key"),

		signatureSecret: cmd.String("signature-secret"),
	}, nil
}

func runServer(_ context.Context, cmd *cli.Command) error {
	port := cmd.Int("port")

	matchmaker, err := newMatchmaker(cmd)
	if err != nil {
		return fmt.Errorf("couldn't create matchmaker service: %w", err)
	}

	defer matchmaker.valkeyClient.Close()

	e := echo.New()

	e.HideBanner = true
	e.HidePort = false

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${id} ${remote_ip} ${status} ${method} ${path} ${error} ${latency_human} ${bytes_in} ${bytes_out}\n",
	}))
	e.Use(middleware.Recover())

	e.GET("/match-up", matchmaker.GetMatchUp)
	e.POST("/outcome", matchmaker.PostOutcome)

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
						Sources: cli.EnvVars("MATCHMAKER_PORT"),
					},
					&cli.StringFlag{
						Name:    "assets-key",
						Value:   "metadata:assets",
						Sources: cli.EnvVars("MATCHMAKER_ASSETS_KEY"),
					},
					&cli.StringFlag{
						Name:    "ratelimit-key",
						Value:   "matchmaker:ratelimit",
						Sources: cli.EnvVars("MATCHMAKER_RATELIMIT_KEY"),
					},
					&cli.StringFlag{
						Name:    "outcomes-key",
						Value:   "matchmaker:outcomes",
						Sources: cli.EnvVars("MATCHMAKER_OUTCOMES_KEY"),
					},
					&cli.StringFlag{
						Name:    "signature-secret",
						Value:   "secret",
						Sources: cli.EnvVars("MATCHMAKER_SIGNATURE_SECRET"),
					},
					&cli.StringFlag{
						Name:    "valkey-addr",
						Value:   "valkey:6379",
						Sources: cli.EnvVars("MATCHMAKER_VALKEY_ADDR"),
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

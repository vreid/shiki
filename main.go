package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/samber/do/v2"
	"github.com/vreid/shiki/internal/pkg/common"
	"github.com/vreid/shiki/internal/pkg/matchmaker"
	"github.com/vreid/shiki/internal/pkg/scorer"

	"github.com/urfave/cli/v3"
)

type ShikiService struct {
	EchoService *common.EchoService `do:""`

	MatchmakerService *matchmaker.MatchmakerService `do:""`
	ScorerService     *scorer.ScorerService         `do:""`
}

func runServer(_ context.Context, cmd *cli.Command) error {
	i := do.New()

	do.ProvideNamedValue(i, "port", cmd.Int("port"))
	do.ProvideNamedValue(i, "tmp-dir", cmd.String("tmp-dir"))

	do.ProvideNamedValue(i, "signature-secret", cmd.String("signature-secret"))
	do.ProvideNamedValue(i, "base-difficulty", cmd.Int("base-difficulty"))
	do.ProvideNamedValue(i, "opponents", cmd.Int("opponents"))
	do.ProvideNamedValue(i, "token-max-age-minutes", cmd.Int("token-max-age-minutes"))

	outcomeChan := make(chan matchmaker.Outcome, 1000)
	var outcomeSource <-chan matchmaker.Outcome = outcomeChan
	var outcomeSink chan<- matchmaker.Outcome = outcomeChan

	do.ProvideNamedValue(i, "outcome-source", outcomeSource)
	do.ProvideNamedValue(i, "outcome-sink", outcomeSink)

	do.Provide(i, common.NewEchoService)

	do.Provide(i, matchmaker.NewMatchmakerService)
	do.Provide(i, scorer.NewScorerService)

	do.Provide(i, do.InvokeStruct[ShikiService])

	shikiService, err := do.Invoke[ShikiService](i)
	if err != nil {
		return fmt.Errorf("failed to create echo service: %w", err)
	}

	shikiService.ScorerService.Start()

	//nolint:wrapcheck
	return shikiService.EchoService.Start()
}

func main() {
	//nolint:exhaustruct
	cmd := &cli.Command{
		Name: "shiki",
		Commands: []*cli.Command{
			{
				Name: "server",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "port",
						Value:   3000, //nolint:mnd
						Sources: cli.EnvVars("SHIKI_PORT"),
					},
					&cli.StringFlag{
						Name:    "tmp-dir",
						Value:   "./shiki/tmp",
						Sources: cli.EnvVars("SHIKI_TMP_DIR"),
					},
					&cli.StringFlag{
						Name:    "signature-secret",
						Value:   "secret",
						Sources: cli.EnvVars("SHIKI_SIGNATURE_SECRET"),
					},
					&cli.IntFlag{
						Name:    "base-difficulty",
						Value:   0,
						Sources: cli.EnvVars("SHIKI_BASE_DIFFICULTY"),
					},
					&cli.IntFlag{
						Name:    "opponents",
						Value:   3,
						Sources: cli.EnvVars("SHIKI_OPPONENTS"),
					},
					&cli.IntFlag{
						Name:    "token-max-age-minutes",
						Value:   5,
						Sources: cli.EnvVars("SHIKI_TOKEN_MAX_AGE_MINUTES"),
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

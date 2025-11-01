package common

import (
	"context"
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/samber/do/v2"
)

type EchoService struct {
	echo *echo.Echo
	port int
}

func NewEchoService(i do.Injector) (*EchoService, error) {
	port := do.MustInvokeNamed[int](i, "port")

	e := echo.New()

	e.HideBanner = true
	e.HidePort = false

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${id} ${remote_ip} ${status} ${method} ${path} ${error} ${latency_human} ${bytes_in} ${bytes_out}\n",
	}))
	e.Use(middleware.Recover())

	return &EchoService{
		echo: e,
		port: port,
	}, nil
}

func (s *EchoService) Register(c func(e *echo.Echo)) {
	c(s.echo)
}

func (s *EchoService) Start() error {
	err := s.echo.Start(fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to start echo server: %w", err)
	}

	return nil
}

func (s *EchoService) Shutdown(ctx context.Context) error {
	err := s.echo.Shutdown(ctx)
	if err != nil {
		return fmt.Errorf("failed to shutdown echo server: %w", err)
	}

	return nil
}

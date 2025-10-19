package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

var (
	port = flag.Int("port", 3000, "")
)

func main() {
	flag.Parse()

	e := echo.New()

	e.HideBanner = true
	e.HidePort = true

	e.GET("/", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	e.Logger.Fatal(e.Start(fmt.Sprintf(":%d", *port)))
}

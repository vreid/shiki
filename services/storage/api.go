package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/labstack/echo/v4"
)

func (x *storage) serveImage(c echo.Context) error {
	id := c.Param("id")
	size := c.Param("size")

	if id == "" || size == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "uuid and size are required")
	}

	var filename string
	if size == "1024" {
		filename = fmt.Sprintf("%s_%s.webp", id, size)
	} else {
		filename = fmt.Sprintf("%s_%s_50.webp", id, size)
	}

	imagePath := filepath.Join(x.dataDir, id, filename)

	_, err := os.Stat(imagePath)
	if os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "image not found")
	}

	//nolint:wrapcheck
	return c.File(imagePath)
}

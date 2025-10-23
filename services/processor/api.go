package main

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/labstack/echo/v4"
)

func (x *processor) listDirectory(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	dirPath := filepath.Join(x.dataDir, id)

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "directory not found")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read directory")
	}

	result := make([]DirectoryEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		result = append(result, DirectoryEntry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Size:  info.Size(),
		})
	}

	//nolint:wrapcheck
	return c.JSONPretty(http.StatusOK, result, "  ")
}

func (x *processor) deleteDirectory(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	dirPath := filepath.Join(x.dataDir, id)

	err := os.RemoveAll(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "directory not found")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete directory")
	}

	//nolint:wrapcheck
	return c.NoContent(http.StatusNoContent)
}

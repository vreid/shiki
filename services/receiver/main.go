package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/valkey-io/valkey-go"
)

var (
	port       = flag.Int("port", 3000, "")
	dataDir    = flag.String("data-dir", "/data", "")
	valkeyAddr = flag.String("valkey-addr", "valkey:6379", "")

	valkeyClient valkey.Client
)

//nolint:tagliatelle
type UploadIndex struct {
	UploadID  string    `json:"upload_id"`
	Timestamp time.Time `json:"timestamp"`
	Files     []string  `json:"files"`
}

func deleteUpload(c echo.Context) error {
	uploadID := c.Param("id")
	if uploadID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "upload_id is required")
	}

	uploadDir := filepath.Join(*dataDir, uploadID)

	err := os.RemoveAll(uploadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "upload not found")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete upload")
	}

	return c.NoContent(http.StatusNoContent)
}

func list(c echo.Context) error {
	entries, err := os.ReadDir(*dataDir)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read data directory")
	}

	directories := make([]string, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, entry.Name())
		}
	}

	//nolint:wrapcheck
	return c.JSON(http.StatusOK, directories)
}

//nolint:cyclop,funlen
func upload(c echo.Context) error {
	form, err := c.MultipartForm()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to parse multipart form")
	}

	files := form.File["files"]

	_uploadID, err := uuid.NewV7()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate UUID")
	}

	uploadID := _uploadID.String()
	uploadDir := filepath.Join(*dataDir, uploadID)

	err = os.MkdirAll(uploadDir, 0700)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create upload directory")
	}

	index := UploadIndex{
		UploadID:  uploadID,
		Timestamp: time.Now(),
		Files:     make([]string, 0, len(files)),
	}

	for _, file := range files {
		src, err := file.Open()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to open uploaded file")
		}

		defer func() {
			_ = src.Close()
		}()

		dstPath := filepath.Join(uploadDir, file.Filename)

		//nolint:gosec
		dst, err := os.Create(dstPath)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to create file")
		}

		defer func() {
			_ = dst.Close()
		}()

		_, err = io.Copy(dst, src)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to write file")
		}

		index.Files = append(index.Files, file.Filename)
	}

	indexPath := filepath.Join(uploadDir, "index.json")

	indexData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to marshal index")
	}

	err = os.WriteFile(indexPath, indexData, 0600)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to write index file")
	}

	ctx := c.Request().Context()

	err = valkeyClient.Do(ctx, valkeyClient.B().Xadd().
		Key("uploads").
		Id("*").
		FieldValue().
		FieldValue("upload_id", uploadID).
		FieldValue("timestamp", strconv.FormatInt(index.Timestamp.Unix(), 10)).
		FieldValue("files", strconv.Itoa(len(index.Files))).
		Build()).Error()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to publish upload event")
	}

	//nolint:wrapcheck
	return c.JSON(http.StatusAccepted, uploadID)
}

func main() {
	flag.Parse()

	var err error

	valkeyClient, err = valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{*valkeyAddr},
	})
	if err != nil {
		panic(err)
	}

	defer valkeyClient.Close()

	e := echo.New()

	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${id} ${remote_ip} ${status} ${method} ${path} ${error} ${latency_human} ${bytes_in} ${bytes_out}\n",
	}))
	e.Use(middleware.Recover())

	e.GET("/", list)
	e.POST("/upload", upload)
	e.DELETE("/:id", deleteUpload)
	e.Static("/", *dataDir)

	e.Logger.Fatal(e.Start(fmt.Sprintf(":%d", *port)))
}

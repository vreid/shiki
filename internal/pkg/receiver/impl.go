package receiver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/do/v2"
	"github.com/vreid/shiki/internal/pkg/common"
)

type ReceiverService struct {
	DatabaseService *common.DatabaseService

	TmpDir string
}

func NewReceiverService(i do.Injector) (*ReceiverService, error) {
	databaseService := do.MustInvoke[*common.DatabaseService](i)
	tmpDir := do.MustInvokeNamed[string](i, "tmp-dir")

	result := &ReceiverService{
		DatabaseService: databaseService,
		TmpDir:          tmpDir,
	}

	echoService, err := do.Invoke[*common.EchoService](i)
	if err != nil {
		return nil, fmt.Errorf("failed to create echo service: %w", err)
	}

	echoService.Register(func(e *echo.Echo) {
		apiGroup := e.Group("/api")

		receiverGroup := apiGroup.Group("/receiver")

		receiverGroup.POST("/upload", result.Upload)
	})

	return result, nil
}

//nolint:cyclop,funlen
func (s *ReceiverService) Upload(c echo.Context) error {
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
	uploadDir := filepath.Join(s.TmpDir, uploadID)

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

	//nolint:wrapcheck
	return c.JSON(http.StatusAccepted, uploadID)
}

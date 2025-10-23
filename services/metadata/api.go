package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/vreid/shiki/libs/go/types"

	"github.com/labstack/echo/v4"
	"go.etcd.io/bbolt"
)

const bucketName = "metadata"

func (x *metadata) list(c echo.Context) error {
	result := []string{}

	err := x.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketName))
		if bucket == nil {
			return nil
		}

		return bucket.ForEach(func(k, _ []byte) error {
			result = append(result, string(k))

			return nil
		})
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list")
	}

	//nolint:wrapcheck
	return c.JSONPretty(http.StatusOK, result, "  ")
}

func (x *metadata) get(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	var data types.Metadata

	err := x.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketName))
		if bucket == nil {
			return echo.NewHTTPError(http.StatusNotFound, "metadata not found")
		}

		value := bucket.Get([]byte(id))
		if value == nil {
			return echo.NewHTTPError(http.StatusNotFound, "metadata not found")
		}

		return json.Unmarshal(value, &data)
	})
	if err != nil {
		var httpErr *echo.HTTPError
		if errors.As(err, &httpErr) {
			return httpErr
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retrieve metadata")
	}

	//nolint:wrapcheck
	return c.JSONPretty(http.StatusOK, data, "  ")
}

func (x *metadata) create(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	var data types.Metadata

	err := c.Bind(&data)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	err = x.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("metadata"))
		if err != nil {
			return fmt.Errorf("failed to open bucket: %w", err)
		}

		if bucket.Get([]byte(id)) != nil {
			return echo.NewHTTPError(http.StatusConflict, "metadata already exists")
		}

		jsonData, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		return bucket.Put([]byte(id), jsonData)
	})
	if err != nil {
		var httpErr *echo.HTTPError
		if errors.As(err, &httpErr) {
			return httpErr
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create metadata")
	}

	//nolint:wrapcheck
	return c.JSONPretty(http.StatusCreated, data, "  ")
}

func (x *metadata) update(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	var data types.Metadata

	err := c.Bind(&data)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}

	err = x.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketName))
		if bucket == nil {
			return echo.NewHTTPError(http.StatusNotFound, "metadata not found")
		}

		if bucket.Get([]byte(id)) == nil {
			return echo.NewHTTPError(http.StatusNotFound, "metadata not found")
		}

		jsonData, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		return bucket.Put([]byte(id), jsonData)
	})
	if err != nil {
		var httpErr *echo.HTTPError
		if errors.As(err, &httpErr) {
			return httpErr
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update metadata")
	}

	//nolint:wrapcheck
	return c.JSONPretty(http.StatusOK, data, "  ")
}

func (x *metadata) delete(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	err := x.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketName))
		if bucket == nil {
			return echo.NewHTTPError(http.StatusNotFound, "metadata not found")
		}

		if bucket.Get([]byte(id)) == nil {
			return echo.NewHTTPError(http.StatusNotFound, "metadata not found")
		}

		return bucket.Delete([]byte(id))
	})
	if err != nil {
		var httpErr *echo.HTTPError
		if errors.As(err, &httpErr) {
			return httpErr
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete metadata")
	}

	//nolint:wrapcheck
	return c.NoContent(http.StatusNoContent)
}

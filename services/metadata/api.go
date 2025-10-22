package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"go.etcd.io/bbolt"
)

func (x *metadata) list(c echo.Context) error {
	db := x.db

	uuids := []string{}

	err := db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("metadata"))
		if bucket == nil {
			return nil
		}

		return bucket.ForEach(func(k, _ []byte) error {
			uuids = append(uuids, string(k))

			return nil
		})
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list UUIDs")
	}

	return c.JSONPretty(http.StatusOK, uuids, "  ")
}

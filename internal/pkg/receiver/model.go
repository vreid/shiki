package receiver

import "time"

type UploadIndex struct {
	UploadID  string    `json:"upload_id"`
	Timestamp time.Time `json:"timestamp"`
	Files     []string  `json:"files"`
}

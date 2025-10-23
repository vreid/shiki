package types

import "time"

type UploadIndex struct {
	UploadID  string    `json:"upload_id"`
	Timestamp time.Time `json:"timestamp"`
	Files     []string  `json:"files"`
}

type Metadata struct {
	OriginalFilename string `json:"original_filename"`
	SHA256           string `json:"sha256"`
	SHA256Strip      string `json:"sha256_strip"`
	SHA256Webp       string `json:"sha256_webp"`
	UUID             string `json:"uuid"`
}

type DirectoryListing struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

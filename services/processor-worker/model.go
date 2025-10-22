package main

import "time"

type UploadIndex struct {
	UploadID  string    `json:"upload_id"`
	Timestamp time.Time `json:"timestamp"`
	Files     []string  `json:"files"`
}

type ScriptResult struct {
	Success string `json:"success"`
	Error   string `json:"error"`
}

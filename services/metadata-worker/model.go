package main

type Metadata struct {
	OriginalFilename string `json:"original_filename"`
	SHA256           string `json:"sha256"`
	SHA256Strip      string `json:"sha256_strip"`
	SHA256Webp       string `json:"sha256_webp"`
	UUID             string `json:"uuid"`
}

package nostr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BlobUploader handles uploading blobs to Blossom servers.
// Blossom is a content-addressed blob storage protocol complementary to Nostr.
// See: https://github.com/hzrd149/blossom
type BlobUploader struct {
	servers    []string
	httpClient *http.Client
}

// BlobUploadResult is the response from a successful Blossom upload.
type BlobUploadResult struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
	Type   string `json:"type"`
}

// NewBlobUploader creates a blob uploader for the given Blossom servers.
func NewBlobUploader(servers []string) *BlobUploader {
	return &BlobUploader{
		servers: servers,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Upload uploads data to configured Blossom servers.
// Returns the first successful upload result. Data is content-addressed by SHA-256.
func (u *BlobUploader) Upload(ctx context.Context, data []byte, contentType string) (*BlobReference, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no blossom servers configured")
	}

	// Compute SHA-256 hash
	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])

	// Try each server until one succeeds
	var lastErr error
	for _, server := range u.servers {
		ref, err := u.uploadToServer(ctx, server, data, contentType, hashHex)
		if err == nil {
			return ref, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("all blossom servers failed, last error: %w", lastErr)
}

// uploadToServer uploads to a single Blossom server.
func (u *BlobUploader) uploadToServer(ctx context.Context, server string, data []byte, contentType, hashHex string) (*BlobReference, error) {
	// Blossom PUT /upload with SHA-256 header
	url := server + "/upload"

	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-SHA-256", hashHex)

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("uploading to %s: %w", server, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upload to %s failed %d: %s", server, resp.StatusCode, string(body))
	}

	// Parse response to get URL
	var result struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Fall back to constructing URL from server + hash
		result.URL = server + "/" + hashHex
		result.SHA256 = hashHex
	}

	return &BlobReference{
		URL:    result.URL,
		SHA256: hashHex,
		Size:   len(data),
		Type:   contentType,
	}, nil
}

// Check verifies if a blob already exists on a Blossom server.
func (u *BlobUploader) Check(ctx context.Context, hashHex string) (string, error) {
	for _, server := range u.servers {
		url := server + "/" + hashHex
		req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
		if err != nil {
			continue
		}

		resp, err := u.httpClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return url, nil
		}
	}
	return "", fmt.Errorf("blob %s not found on any server", hashHex)
}

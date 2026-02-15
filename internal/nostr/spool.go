package nostr

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fiatjaf.com/nostr"
)

// Spool is a local event store for offline resilience.
// When relay connectivity fails, events are spooled here and drained later.
//
// File format: one JSON object per line (JSONL) at ~/gt/.runtime/nostr-spool.jsonl
// Archive: old events (>24h) are moved to nostr-spool-archive.jsonl
type Spool struct {
	mu          sync.Mutex
	path        string // active spool file
	archivePath string // archive file for old events
	softLimit   int    // warning threshold (default: 10,000)
	hardLimit   int    // stop threshold (default: 100,000)
}

// SpoolEntry is a single spooled event with retry metadata.
type SpoolEntry struct {
	// Embed the nostr event fields
	ID        string     `json:"id"`
	CreatedAt int64      `json:"created_at"`
	Kind      int        `json:"kind"`
	Tags      nostr.Tags `json:"tags"`
	Content   string     `json:"content"`
	PubKey    string     `json:"pubkey"`
	Sig       string     `json:"sig"`

	// Spool metadata
	SpoolMeta SpoolMeta `json:"spool_meta"`
}

// SpoolMeta contains retry tracking information.
type SpoolMeta struct {
	SpooledAt    time.Time `json:"spooled_at"`
	TargetRelays []string  `json:"target_relays"`
	Attempts     int       `json:"attempts"`
	LastAttempt  *time.Time `json:"last_attempt"`
	LastError    *string   `json:"last_error"`
}

// Default spool limits.
const (
	DefaultSpoolSoftLimit = 10000
	DefaultSpoolHardLimit = 100000
	SpoolFileName         = "nostr-spool.jsonl"
	SpoolArchiveFileName  = "nostr-spool-archive.jsonl"
	SpoolMaxAge           = 24 * time.Hour
)

// NewSpool creates a new spool in the given runtime directory.
func NewSpool(runtimeDir string) *Spool {
	return &Spool{
		path:        filepath.Join(runtimeDir, SpoolFileName),
		archivePath: filepath.Join(runtimeDir, SpoolArchiveFileName),
		softLimit:   DefaultSpoolSoftLimit,
		hardLimit:   DefaultSpoolHardLimit,
	}
}

// Enqueue adds an event to the spool.
// Returns an error if the hard limit is exceeded.
func (s *Spool) Enqueue(event *nostr.Event, targetRelays []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check hard limit
	count := s.countLocked()
	if count >= s.hardLimit {
		return fmt.Errorf("spool hard limit exceeded (%d events); require operator intervention", count)
	}
	if count >= s.softLimit {
		log.Printf("[nostr] spool soft limit reached (%d events)", count)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("creating spool directory: %w", err)
	}

	// Create entry
	entry := SpoolEntry{
		ID:        IDToString(event.ID),
		CreatedAt: int64(event.CreatedAt),
		Kind:      int(event.Kind),
		Tags:      event.Tags,
		Content:   event.Content,
		PubKey:    PubKeyToString(event.PubKey),
		Sig:       event.Sig,
		SpoolMeta: SpoolMeta{
			SpooledAt:    time.Now(),
			TargetRelays: targetRelays,
			Attempts:     0,
		},
	}

	// Append to spool file
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("opening spool file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling spool entry: %w", err)
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing spool entry: %w", err)
	}

	return nil
}

// Drain attempts to send all spooled events to relays.
// Successfully sent events are removed from the spool.
// Failed events remain with updated attempt counts.
//
// Implements exponential backoff: events that have failed recently
// are skipped based on their attempt count.
func (s *Spool) Drain(ctx context.Context, pool *RelayPool) (sent int, failed int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readAllLocked()
	if err != nil {
		return 0, 0, err
	}

	if len(entries) == 0 {
		return 0, 0, nil
	}

	now := time.Now()
	var remaining []SpoolEntry

	for _, entry := range entries {
		// Check exponential backoff
		if entry.SpoolMeta.LastAttempt != nil {
			backoff := backoffDuration(entry.SpoolMeta.Attempts)
			if now.Sub(*entry.SpoolMeta.LastAttempt) < backoff {
				remaining = append(remaining, entry)
				continue
			}
		}

		// Reconstruct nostr event
		// Reconstruct event from spool entry
		var id nostr.ID
		if b, err := hex.DecodeString(entry.ID); err == nil && len(b) == len(id) {
			copy(id[:], b)
		}
		event := nostr.Event{
			ID:        id,
			CreatedAt: nostr.Timestamp(entry.CreatedAt),
			Kind:      nostr.Kind(entry.Kind),
			Tags:      entry.Tags,
			Content:   entry.Content,
			PubKey:    PubKeyFromHex(entry.PubKey),
			Sig:       entry.Sig,
		}

		// Try to publish
		if pubErr := pool.Publish(ctx, event); pubErr != nil {
			// Update attempt metadata
			entry.SpoolMeta.Attempts++
			nowCopy := now
			entry.SpoolMeta.LastAttempt = &nowCopy
			errStr := pubErr.Error()
			entry.SpoolMeta.LastError = &errStr
			remaining = append(remaining, entry)
			failed++
		} else {
			sent++
		}
	}

	// Rewrite spool file with remaining entries
	if err := s.writeAllLocked(remaining); err != nil {
		return sent, failed, fmt.Errorf("rewriting spool: %w", err)
	}

	return sent, failed, nil
}

// Count returns the number of events in the spool.
func (s *Spool) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.countLocked()
}

// ArchiveOld moves events older than maxAge to the archive file.
func (s *Spool) ArchiveOld(maxAge time.Duration) (archived int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readAllLocked()
	if err != nil {
		return 0, err
	}

	now := time.Now()
	var active, old []SpoolEntry

	for _, entry := range entries {
		if now.Sub(entry.SpoolMeta.SpooledAt) > maxAge {
			old = append(old, entry)
		} else {
			active = append(active, entry)
		}
	}

	if len(old) == 0 {
		return 0, nil
	}

	// Append old entries to archive
	archiveFile, err := os.OpenFile(s.archivePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("opening archive file: %w", err)
	}
	defer archiveFile.Close()

	for _, entry := range old {
		data, _ := json.Marshal(entry)
		archiveFile.Write(append(data, '\n'))
	}

	// Rewrite active spool
	if err := s.writeAllLocked(active); err != nil {
		return 0, fmt.Errorf("rewriting spool: %w", err)
	}

	return len(old), nil
}

// --- Internal helpers ---

func (s *Spool) countLocked() int {
	f, err := os.Open(s.path)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	return count
}

func (s *Spool) readAllLocked() ([]SpoolEntry, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening spool: %w", err)
	}
	defer f.Close()

	var entries []SpoolEntry
	scanner := bufio.NewScanner(f)
	// Allow large lines (events can be big)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry SpoolEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			log.Printf("[nostr] skipping malformed spool entry: %v", err)
			continue
		}
		entries = append(entries, entry)
	}

	return entries, scanner.Err()
}

func (s *Spool) writeAllLocked(entries []SpoolEntry) error {
	// Use 0600 permissions for spool files (contain event data)
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		f.Write(append(data, '\n'))
	}

	return nil
}

// backoffDuration returns the backoff duration for a given attempt count.
// Uses exponential backoff: 30s, 60s, 120s, 300s cap.
func backoffDuration(attempts int) time.Duration {
	switch {
	case attempts <= 0:
		return 0
	case attempts == 1:
		return 30 * time.Second
	case attempts == 2:
		return 60 * time.Second
	case attempts == 3:
		return 120 * time.Second
	default:
		return 300 * time.Second // cap at 5 minutes
	}
}
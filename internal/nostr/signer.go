package nostr

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip46"
)

// Signer signs Nostr events. All signing in Gas Town goes through this interface
// so that the signing backend (NIP-46 bunker vs local key) can be swapped.
type Signer interface {
	// Sign computes the event ID, sets the pubkey, and signs the event.
	Sign(ctx context.Context, event *nostr.Event) error

	// GetPublicKey returns the signer's public key as a hex string.
	GetPublicKey() string

	// Close releases any resources (e.g., bunker connection).
	Close() error
}

// --- NIP-46 Signer (production) ---

// NIP46Signer signs events via an external NIP-46 bunker.
// This is the production signing path — no secret keys are stored on disk.
type NIP46Signer struct {
	mu        sync.Mutex
	bunkerURI string
	pubkey    string
	bunker    *nip46.BunkerClient
}

// NewNIP46Signer creates a signer that connects to a NIP-46 bunker.
// The bunkerURI format is: bunker://npub1...?relay=wss://...
func NewNIP46Signer(ctx context.Context, bunkerURI string) (*NIP46Signer, error) {
	if !strings.HasPrefix(bunkerURI, "bunker://") {
		return nil, fmt.Errorf("invalid bunker URI: must start with bunker://")
	}

	// ConnectBunker requires: ctx, clientSecretKey, bunkerURI, pool, statusCallback
	// Generate an ephemeral client secret key for the NIP-46 connection.
	clientKeyHex := nostr.GeneratePrivateKey()
	var clientKey nostr.SecretKey
	if b, err := hex.DecodeString(clientKeyHex); err == nil && len(b) == len(clientKey) {
		copy(clientKey[:], b)
	} else {
		return nil, fmt.Errorf("generating client key: invalid hex from GeneratePrivateKey")
	}
	bunker, err := nip46.ConnectBunker(ctx, clientKey, bunkerURI, nil, func(status string) {
		log.Printf("[nostr/signer] bunker status: %s", status)
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to bunker: %w", err)
	}

	pubkey, err := bunker.GetPublicKey(ctx)
	if err != nil {
		bunker.Close()
		return nil, fmt.Errorf("getting public key from bunker: %w", err)
	}

	return &NIP46Signer{
		bunkerURI: bunkerURI,
		pubkey:    pubkey,
		bunker:    bunker,
	}, nil
}

// Sign signs an event using the NIP-46 bunker.
func (s *NIP46Signer) Sign(ctx context.Context, event *nostr.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	event.PubKey = PubKeyFromHex(s.pubkey)

	return s.bunker.SignEvent(ctx, event)
}

// GetPublicKey returns the signer's public key.
func (s *NIP46Signer) GetPublicKey() string {
	return s.pubkey
}

// Close disconnects from the bunker.
func (s *NIP46Signer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bunker != nil {
		s.bunker.Close()
		s.bunker = nil
	}
	return nil
}

// --- Local Signer (development/testing) ---

// LocalSigner signs events with a local private key.
// This is for development and testing only — production MUST use NIP-46.
type LocalSigner struct {
	privkey    string         // hex-encoded private key
	secretKey  nostr.SecretKey // decoded secret key for signing
	pubkey     string         // hex-encoded public key
}

// NewLocalSigner creates a signer from a hex-encoded private key.
// WARNING: This stores a secret key in memory. Use only for testing.
func NewLocalSigner(privkeyHex string) (*LocalSigner, error) {
	// Decode hex to SecretKey byte array first
	var sk nostr.SecretKey
	if b, decErr := hex.DecodeString(privkeyHex); decErr == nil && len(b) == len(sk) {
		copy(sk[:], b)
	} else {
		return nil, fmt.Errorf("invalid private key hex: %v", decErr)
	}
	pubkeyResult, err := nostr.GetPublicKey(sk)
	if err != nil {
		return nil, fmt.Errorf("deriving public key: %w", err)
	}
	// GetPublicKey may return string or PubKey depending on library version.
	// We store as hex string for our interface.
	pubkey := fmt.Sprintf("%x", pubkeyResult)
	return &LocalSigner{
		privkey:   privkeyHex,
		secretKey: sk,
		pubkey:    pubkey,
	}, nil
}

// Sign signs an event with the local private key.
func (s *LocalSigner) Sign(_ context.Context, event *nostr.Event) error {
	event.PubKey = PubKeyFromHex(s.pubkey)
	return event.Sign(s.secretKey)
}

// GetPublicKey returns the signer's public key.
func (s *LocalSigner) GetPublicKey() string {
	return s.pubkey
}

// Close is a no-op for local signers.
func (s *LocalSigner) Close() error {
	return nil
}
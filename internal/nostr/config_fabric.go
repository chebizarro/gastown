package nostr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	fnostr "fiatjaf.com/nostr"
	cascadia "git.sharegap.net/cascadia/cascadia-nips/generated/go"
	"github.com/steveyegge/gastown/internal/config"
)

const (
	ConfigServiceID    = "gastown"
	ConfigPolicyName   = "rate-limits"
	ConfigPolicySchema = "cascadia.config.rate-limits.v1"
	ConfigStatusSchema = "cascadia.config.status.v1"
)

var configCoordinate = "service:" + ConfigServiceID + ":" + ConfigPolicyName

type configStatusPublisher interface {
	PublishReplaceable(context.Context, *fnostr.Event) error
}
type ConfigFabric struct {
	path      string
	apply     func(*config.NostrConfig) error
	publisher configStatusPublisher
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewConfigFabric(ctx context.Context, path, runtimeDir string, apply func(*config.NostrConfig) error) (*ConfigFabric, error) {
	current, err := config.LoadOrCreateNostrConfig(path)
	if err != nil {
		return nil, err
	}
	if current.ConfigFabric == nil {
		return nil, errors.New("config fabric trusted authors and subscription relays are required")
	}
	identity := current.Identities["deacon"]
	if identity == nil || strings.TrimSpace(identity.Signer.Bunker) == "" {
		return nil, errors.New("config fabric requires the existing deacon NIP-46 signing identity")
	}
	signer, err := NewNIP46Signer(ctx, identity.Signer.Bunker)
	if err != nil {
		return nil, err
	}
	control := *current
	control.Enabled = true
	control.ReadRelays = append([]string(nil), current.ConfigFabric.SubscriptionRelays...)
	control.WriteRelays = append([]string(nil), current.ConfigFabric.SubscriptionRelays...)
	publisher, err := NewPublisher(ctx, &control, signer, runtimeDir)
	if err != nil {
		_ = signer.Close()
		return nil, err
	}
	return &ConfigFabric{path: path, apply: apply, publisher: publisher}, nil
}
func newConfigFabricForTest(path string, apply func(*config.NostrConfig) error, publisher configStatusPublisher) *ConfigFabric {
	return &ConfigFabric{path: path, apply: apply, publisher: publisher}
}
func (f *ConfigFabric) Start(ctx context.Context) {
	f.mu.Lock()
	f.ctx = ctx
	f.mu.Unlock()
	f.restart()
}
func (f *ConfigFabric) Close() {
	f.mu.Lock()
	if f.cancel != nil {
		f.cancel()
	}
	f.mu.Unlock()
	if p, ok := f.publisher.(*Publisher); ok {
		_ = p.Close()
	}
}
func (f *ConfigFabric) restart() {
	f.mu.Lock()
	if f.ctx == nil {
		f.mu.Unlock()
		return
	}
	if f.cancel != nil {
		f.cancel()
	}
	runCtx, cancel := context.WithCancel(f.ctx)
	f.cancel = cancel
	f.mu.Unlock()
	current, err := config.LoadOrCreateNostrConfig(f.path)
	if err != nil {
		log.Printf("[nostr/config] reload subscription policy: %v", err)
		return
	}
	for _, relayURL := range current.ConfigFabric.SubscriptionRelays {
		go f.subscribeRelay(runCtx, relayURL, current.ConfigFabric.TrustedAuthors)
	}
}
func (f *ConfigFabric) subscribeRelay(ctx context.Context, relayURL string, trusted []string) {
	authors := make([]fnostr.PubKey, 0, len(trusted))
	for _, author := range trusted {
		if pk, err := fnostr.PubKeyFromHex(author); err == nil {
			authors = append(authors, pk)
		}
	}
	delay := time.Second
	for ctx.Err() == nil {
		relay, err := fnostr.RelayConnect(ctx, relayURL, fnostr.RelayOptions{})
		if err == nil {
			sub, subErr := relay.Subscribe(ctx, fnostr.Filter{Kinds: []fnostr.Kind{fnostr.Kind(cascadia.NIP78_APP_DATA)}, Authors: authors, Tags: fnostr.TagMap{"d": []string{configCoordinate}}}, fnostr.SubscriptionOptions{})
			if subErr == nil {
				delay = time.Second
				for {
					select {
					case event, ok := <-sub.Events:
						if !ok {
							goto reconnect
						}
						if _, err := f.Handle(ctx, &event); err != nil {
							log.Printf("[nostr/config] event handling: %v", err)
						}
					case reason := <-sub.ClosedReason:
						log.Printf("[nostr/config] subscription closed by %s: %s", relayURL, reason)
						goto reconnect
					case <-ctx.Done():
						sub.Unsub()
						_ = relay.Close()
						return
					}
				}
			}
		reconnect:
			if sub != nil {
				sub.Unsub()
			}
			_ = relay.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			if delay < 30*time.Second {
				delay *= 2
			}
		}
	}
}
func (f *ConfigFabric) Handle(ctx context.Context, event *fnostr.Event) (bool, error) {
	current, err := config.LoadOrCreateNostrConfig(f.path)
	if err != nil {
		return false, err
	}
	version, schema := positiveConfigVersion(event), configTagOr(event, "schema", ConfigPolicySchema)
	reject := func(reason string) (bool, error) {
		return false, f.publishStatus(ctx, event, cascadia.CascadiaConfigStatusV1Payload{ServiceId: ConfigServiceID, Scope: configTagOr(event, "scope", current.ConfigFabric.Scope), Version: int(version), PolicySchema: schema, ConfigEventId: safeConfigEventID(IDToString(event.ID)), Status: "rejected", Reason: safeConfigReason(reason)})
	}
	if !event.CheckID() || !event.VerifySignature() {
		return reject("invalid event id or signature")
	}
	author := PubKeyToString(event.PubKey)
	if !stringContains(current.ConfigFabric.TrustedAuthors, author) {
		return reject("author is not trusted")
	}
	if int(event.Kind) != cascadia.NIP78_APP_DATA {
		return reject("unsupported event kind")
	}
	tags, err := parseConfigTags(event)
	if err != nil {
		return reject(err.Error())
	}
	version, schema = tags.version, tags.schema
	if tags.d != configCoordinate {
		return reject("wrong policy coordinate")
	}
	if tags.service != ConfigServiceID {
		return reject("wrong target service")
	}
	if tags.scope != current.ConfigFabric.Scope {
		return reject("wrong target scope")
	}
	if tags.schema != ConfigPolicySchema {
		return reject("unsupported policy schema")
	}
	if accepted := current.ConfigFabric.Accepted; accepted != nil && accepted.Author == author && accepted.Coordinate == tags.d && version <= accepted.Version {
		return reject("stale policy version")
	}
	var generated cascadia.CascadiaConfigRateLimitsV1Payload
	if err := json.Unmarshal([]byte(event.Content), &generated); err != nil {
		return reject("content is not valid JSON")
	}
	if err := generated.Validate(); err != nil {
		return reject("content does not conform to policy schema")
	}
	if generated.ServiceId != tags.service || generated.Scope != tags.scope || int64(generated.Version) != version || generated.Schema != tags.schema {
		return reject("content envelope does not match event tags")
	}
	if configContainsSecret(generated.Policy) {
		return reject("secret values are forbidden in policy content")
	}
	data, err := json.Marshal(generated.Policy)
	if err != nil {
		return reject("policy content cannot be encoded")
	}
	var policy config.NostrRuntimePolicy
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return reject("content does not conform to gastown policy")
	}
	accepted := config.AcceptedDesiredState{EventID: IDToString(event.ID), Author: author, Version: version, Schema: tags.schema, Coordinate: tags.d, AcceptedAt: time.Now().UTC()}
	candidate, err := config.PersistNostrDesiredState(f.path, current, policy, accepted)
	if err != nil {
		return reject("policy persistence failed")
	}
	if f.apply != nil {
		if err := f.apply(candidate); err != nil {
			if restoreErr := config.SaveNostrConfig(f.path, current); restoreErr != nil {
				return reject("policy rollback persistence failed")
			}
			return reject("policy hot apply failed")
		}
	}
	payload := cascadia.CascadiaConfigStatusV1Payload{ServiceId: ConfigServiceID, Scope: tags.scope, Version: int(version), PolicySchema: tags.schema, ConfigEventId: IDToString(event.ID), Status: "applied", EffectiveVersion: int(version), LastAppliedEventId: IDToString(event.ID)}
	if err := f.publishStatus(ctx, event, payload); err != nil {
		log.Printf("[nostr/config] applied status publication: %v", err)
	}
	f.restart()
	return true, nil
}
func (f *ConfigFabric) publishStatus(ctx context.Context, desired *fnostr.Event, payload cascadia.CascadiaConfigStatusV1Payload) error {
	if err := payload.Validate(); err != nil {
		return err
	}
	if payload.Status == "applied" && (payload.EffectiveVersion < 1 || payload.LastAppliedEventId == "") {
		return errors.New("applied status missing effective state")
	}
	if payload.Status == "rejected" && payload.Reason == "" {
		return errors.New("rejected status missing reason")
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	status := &fnostr.Event{Kind: fnostr.Kind(cascadia.CAS_CP_STATE), CreatedAt: fnostr.Now(), Tags: fnostr.Tags{{"d", "config-status:" + ConfigServiceID + ":" + ConfigPolicyName + ":" + payload.Scope}, {"domain", "config-status"}, {"schema", ConfigStatusSchema}, {"status", payload.Status}, {"service", ConfigServiceID}, {"scope", payload.Scope}, {"version", strconv.Itoa(payload.Version)}, {"e", IDToString(desired.ID)}}, Content: string(content)}
	return f.publisher.PublishReplaceable(ctx, status)
}

type parsedConfigTags struct {
	d, service, scope, schema string
	version                   int64
}

func parseConfigTags(event *fnostr.Event) (parsedConfigTags, error) {
	value := func(name string) (string, error) {
		var found []string
		for _, tag := range event.Tags {
			if len(tag) == 2 && tag[0] == name {
				found = append(found, tag[1])
			}
		}
		if len(found) != 1 || found[0] == "" {
			return "", fmt.Errorf("expected exactly one %s tag", name)
		}
		return found[0], nil
	}
	d, err := value("d")
	if err != nil {
		return parsedConfigTags{}, err
	}
	service, err := value("service")
	if err != nil {
		return parsedConfigTags{}, err
	}
	scope, err := value("scope")
	if err != nil {
		return parsedConfigTags{}, err
	}
	versionText, err := value("version")
	if err != nil {
		return parsedConfigTags{}, err
	}
	schema, err := value("schema")
	if err != nil {
		return parsedConfigTags{}, err
	}
	version, err := strconv.ParseInt(versionText, 10, 64)
	if err != nil || version < 1 {
		return parsedConfigTags{}, errors.New("version must be a positive integer")
	}
	return parsedConfigTags{d: d, service: service, scope: scope, schema: schema, version: version}, nil
}
func configTagOr(event *fnostr.Event, name, fallback string) string {
	var result string
	count := 0
	for _, tag := range event.Tags {
		if len(tag) == 2 && tag[0] == name {
			result, count = tag[1], count+1
		}
	}
	if count == 1 && result != "" {
		return result
	}
	return fallback
}
func positiveConfigVersion(event *fnostr.Event) int64 {
	value, err := strconv.ParseInt(configTagOr(event, "version", "1"), 10, 64)
	if err != nil || value < 1 {
		return 1
	}
	return value
}
func stringContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func configContainsSecret(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "private_key") || strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
				return true
			}
			if configContainsSecret(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if configContainsSecret(child) {
				return true
			}
		}
	}
	return false
}
func safeConfigReason(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " ")
	if len(value) > 240 {
		value = value[:240]
	}
	return value
}
func safeConfigEventID(value string) string {
	if len(value) == 64 {
		return value
	}
	return strings.Repeat("0", 64)
}

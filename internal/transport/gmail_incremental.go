package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const maxIncrementalDraftCacheEntries = 5000

type DraftRef struct {
	DraftID   string
	MessageID string
}

type gmailDraftCache struct {
	mu      sync.Mutex
	path    string
	loaded  bool
	entries map[string]Draft
}

type gmailDraftCacheFile struct {
	Schema  string           `json:"schema"`
	Entries map[string]Draft `json:"entries"`
}

var gmailDraftCaches sync.Map

func cacheForGmail(client *GmailClient) *gmailDraftCache {
	value, _ := gmailDraftCaches.LoadOrStore(
		client,
		&gmailDraftCache{entries: make(map[string]Draft)},
	)
	return value.(*gmailDraftCache)
}

func (g *GmailClient) SetDraftCachePath(path string) error {
	cache := cacheForGmail(g)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.path = filepath.Clean(path)
	cache.loaded = false
	cache.entries = make(map[string]Draft)
	return cache.loadLocked()
}

func (c *gmailDraftCache) loadLocked() error {
	if c.loaded {
		return nil
	}
	c.loaded = true
	if c.path == "" {
		return nil
	}
	raw, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Gmail draft cache: %w", err)
	}
	var payload gmailDraftCacheFile
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("decode Gmail draft cache: %w", err)
	}
	if payload.Schema != "cwapi.gmail-draft-cache.v1" || payload.Entries == nil {
		return nil
	}
	for key, draft := range payload.Entries {
		if draft.DraftID == "" || draft.MessageID == "" {
			continue
		}
		c.entries[key] = draft
	}
	return nil
}

func (c *gmailDraftCache) saveLocked() error {
	if c.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("create Gmail draft cache directory: %w", err)
	}
	payload := gmailDraftCacheFile{
		Schema:  "cwapi.gmail-draft-cache.v1",
		Entries: c.entries,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Gmail draft cache: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(c.path), ".gmail-draft-cache-*.tmp")
	if err != nil {
		return fmt.Errorf("create Gmail draft cache temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace Gmail draft cache: %w", err)
	}
	if err := os.Rename(temporaryName, c.path); err != nil {
		return fmt.Errorf("commit Gmail draft cache: %w", err)
	}
	return nil
}

func (c *gmailDraftCache) trimLocked() {
	for len(c.entries) > maxIncrementalDraftCacheEntries {
		for key := range c.entries {
			delete(c.entries, key)
			break
		}
	}
}

func (g *GmailClient) ListDraftRefs(
	ctx context.Context,
	query string,
	maxResults int,
) ([]DraftRef, error) {
	if maxResults < 1 {
		maxResults = 1
	}
	if maxResults > maxDraftScanResults {
		maxResults = maxDraftScanResults
	}

	refs := make([]DraftRef, 0, maxResults)
	pageToken := ""
	for len(refs) < maxResults {
		endpoint, err := url.Parse(g.baseURL + "/users/me/drafts")
		if err != nil {
			return nil, err
		}
		pageSize := maxResults - len(refs)
		if pageSize > 500 {
			pageSize = 500
		}
		values := endpoint.Query()
		values.Set("q", query)
		values.Set("maxResults", strconv.Itoa(pageSize))
		if pageToken != "" {
			values.Set("pageToken", pageToken)
		}
		endpoint.RawQuery = values.Encode()

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		response, err := g.authorizedDo(ctx, request)
		if err != nil {
			return nil, err
		}
		var listing struct {
			Drafts []struct {
				ID      string `json:"id"`
				Message struct {
					ID string `json:"id"`
				} `json:"message"`
			} `json:"drafts"`
			NextPageToken string `json:"nextPageToken"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&listing)
		response.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode draft references: %w", decodeErr)
		}
		for _, item := range listing.Drafts {
			if item.ID == "" {
				continue
			}
			refs = append(refs, DraftRef{
				DraftID:   item.ID,
				MessageID: item.Message.ID,
			})
			if len(refs) >= maxResults {
				return refs, nil
			}
		}
		pageToken = listing.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return refs, nil
}

func (g *GmailClient) ListDraftsIncremental(
	ctx context.Context,
	query string,
	maxResults int,
) ([]Draft, error) {
	refs, err := g.ListDraftRefs(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}
	cache := cacheForGmail(g)
	result := make([]Draft, 0, len(refs))
	changed := false

	cache.mu.Lock()
	if err := cache.loadLocked(); err != nil {
		cache.mu.Unlock()
		return nil, err
	}
	cache.mu.Unlock()

	for _, ref := range refs {
		cacheKey := ref.DraftID + "\x00" + ref.MessageID
		cache.mu.Lock()
		cached, ok := cache.entries[cacheKey]
		cache.mu.Unlock()
		if ok && ref.MessageID != "" {
			result = append(result, cached)
			continue
		}

		draft, getErr := g.GetDraft(ctx, ref.DraftID)
		if getErr != nil {
			return nil, getErr
		}
		actualKey := draft.DraftID + "\x00" + draft.MessageID
		cache.mu.Lock()
		cache.entries[actualKey] = draft
		cache.trimLocked()
		cache.mu.Unlock()
		changed = true
		result = append(result, draft)
	}

	if changed {
		cache.mu.Lock()
		err = cache.saveLocked()
		cache.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

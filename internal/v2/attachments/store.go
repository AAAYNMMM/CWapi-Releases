package attachments

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

type Stored struct {
	Metadata Metadata
	Path     string
}

type Store struct {
	root string
}

func NewTransientStore(dataRoot string) (*Store, error) {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" || !filepath.IsAbs(dataRoot) {
		return nil, errors.New("ATTACHMENT_STORE_ROOT_INVALID")
	}
	root := filepath.Join(filepath.Clean(dataRoot), "temp", "agent-attachments")
	if err := os.RemoveAll(root); err != nil {
		return nil, fmt.Errorf("ATTACHMENT_STORE_CLEAN_FAILED: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("ATTACHMENT_STORE_CREATE_FAILED: %w", err)
	}
	return &Store{root: root}, nil
}

func (s *Store) Put(owner string, batch Batch) ([]Stored, error) {
	if s == nil || s.root == "" {
		return nil, errors.New("ATTACHMENT_STORE_UNAVAILABLE")
	}
	if !ownerPattern.MatchString(owner) {
		return nil, errors.New("ATTACHMENT_STORE_OWNER_INVALID")
	}
	directory := filepath.Join(s.root, owner)
	if err := os.RemoveAll(directory); err != nil {
		return nil, fmt.Errorf("ATTACHMENT_STORE_OWNER_CLEAN_FAILED: %w", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("ATTACHMENT_STORE_OWNER_CREATE_FAILED: %w", err)
	}
	stored := make([]Stored, 0, len(batch.Items))
	for index, item := range batch.Items {
		name := fmt.Sprintf("%03d-%s", index+1, SanitizeFilename(item.Metadata.Name))
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, item.Data, 0o600); err != nil {
			_ = os.RemoveAll(directory)
			return nil, fmt.Errorf("ATTACHMENT_STORE_WRITE_FAILED: %w", err)
		}
		stored = append(stored, Stored{Metadata: item.Metadata, Path: path})
	}
	return stored, nil
}

func (s *Store) Load(stored []Stored) ([]Item, error) {
	if s == nil || s.root == "" {
		return nil, errors.New("ATTACHMENT_STORE_UNAVAILABLE")
	}
	items := make([]Item, 0, len(stored))
	for _, entry := range stored {
		if !underRoot(s.root, entry.Path) {
			return nil, errors.New("ATTACHMENT_STORE_PATH_INVALID")
		}
		data, err := readBounded(entry.Path, entry.Metadata.Size)
		if err != nil {
			return nil, err
		}
		item, err := buildItem(entry.Metadata.Ref, entry.Metadata.Name, entry.Metadata.MIMEType, data, Policy{
			MaxFiles: 1, MaxAttachmentBytes: entry.Metadata.Size, MaxBatchBytes: entry.Metadata.Size,
		})
		if err != nil {
			return nil, err
		}
		if item.Metadata.SHA256 != entry.Metadata.SHA256 || item.Metadata.Size != entry.Metadata.Size {
			return nil, errors.New("ATTACHMENT_STORE_INTEGRITY_FAILED")
		}
		item.Metadata = entry.Metadata
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) Remove(owner string) error {
	if s == nil || s.root == "" {
		return nil
	}
	if !ownerPattern.MatchString(owner) {
		return errors.New("ATTACHMENT_STORE_OWNER_INVALID")
	}
	return os.RemoveAll(filepath.Join(s.root, owner))
}

func (s *Store) Close() error {
	if s == nil || s.root == "" {
		return nil
	}
	return os.RemoveAll(s.root)
}

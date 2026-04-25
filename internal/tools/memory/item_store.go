package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"local-agent/internal/ids"
	"local-agent/internal/security"
)

// ListItems returns parsed memory items from Markdown source files.
func (s *Store) ListItems(filter MemoryItemFilter) ([]MemoryItem, error) {
	paths, err := s.ListFiles()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	items := []MemoryItem{}
	for _, path := range paths {
		file, err := s.ReadFile(path)
		if err != nil {
			return nil, err
		}
		doc := ParseMemoryDocument(file)
		for _, item := range doc.Items {
			item.Path = path
			if !memoryItemMatchesFilter(item, filter, now) {
				continue
			}
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Importance == items[j].Importance {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].Importance > items[j].Importance
	})
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

// SearchItems returns active, non-sensitive items that match the query.
func (s *Store) SearchItems(query string, limit int, projectKey string) ([]MemoryItem, error) {
	items, err := s.ListItems(MemoryItemFilter{
		Query:  query,
		Status: MemoryItemStatusActive,
	})
	if err != nil {
		return nil, err
	}
	if projectKey != "" {
		sort.SliceStable(items, func(i, j int) bool {
			left := items[i].ProjectKey == projectKey
			right := items[j].ProjectKey == projectKey
			if left == right {
				return items[i].Importance > items[j].Importance
			}
			return left
		})
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// GetItem returns one memory item by ID.
func (s *Store) GetItem(id string) (MemoryItem, error) {
	item, _, _, _, err := s.findItem(id)
	return item, err
}

// CreateItem writes a new active memory item to Markdown and reindexes memory.
func (s *Store) CreateItem(ctx context.Context, input MemoryItemCreateInput) (MemoryItem, error) {
	item := MemoryItem{
		ID:              ids.New("mem"),
		Scope:           defaultScope(input.Scope),
		Type:            defaultType(input.Type),
		ProjectKey:      strings.TrimSpace(input.ProjectKey),
		Text:            strings.TrimSpace(input.Text),
		Source:          input.Source,
		SourceMessageID: input.SourceMessageID,
		Confidence:      defaultPositive(input.Confidence, 0.8),
		Importance:      defaultPositive(input.Importance, 0.6),
		Tags:            append([]string(nil), input.Tags...),
		Status:          MemoryItemStatusActive,
		ExpiresAt:       input.ExpiresAt,
		DecayPolicy:     input.DecayPolicy,
		Metadata:        cloneAnyMap(input.Metadata),
		Path:            strings.TrimSpace(input.Path),
	}
	if item.Text == "" {
		return MemoryItem{}, errors.New("memory item text is required")
	}
	scan := ScanSensitiveMemory(item.Text)
	if scan.Sensitive {
		return MemoryItem{}, fmt.Errorf("refusing to store sensitive memory: %s", strings.Join(scan.Findings, ","))
	}
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	item.Path = memoryItemPath(item)

	doc, err := s.readDocumentOrNew(item.Path, item)
	if err != nil {
		return MemoryItem{}, err
	}
	doc.Items = append(doc.Items, item)
	if err := s.writeDocument(ctx, doc); err != nil {
		return MemoryItem{}, err
	}
	return item, nil
}

// UpdateItem mutates an existing Markdown-backed memory item and reindexes.
func (s *Store) UpdateItem(ctx context.Context, id string, input MemoryItemUpdateInput) (MemoryItem, error) {
	item, doc, idx, _, err := s.findItem(id)
	if err != nil {
		return MemoryItem{}, err
	}
	if strings.HasPrefix(item.ID, "legacy_") {
		return MemoryItem{}, errors.New("legacy memory items must be converted before update")
	}
	if input.Text != nil {
		text := strings.TrimSpace(*input.Text)
		if text == "" {
			return MemoryItem{}, errors.New("memory item text cannot be empty")
		}
		scan := ScanSensitiveMemory(text)
		if scan.Sensitive {
			return MemoryItem{}, fmt.Errorf("refusing to store sensitive memory: %s", strings.Join(scan.Findings, ","))
		}
		item.Text = text
	}
	if input.Scope != nil {
		item.Scope = defaultScope(*input.Scope)
	}
	if input.Type != nil {
		item.Type = defaultType(*input.Type)
	}
	if input.ProjectKey != nil {
		item.ProjectKey = strings.TrimSpace(*input.ProjectKey)
	}
	if input.Confidence != nil {
		item.Confidence = *input.Confidence
	}
	if input.Importance != nil {
		item.Importance = *input.Importance
	}
	if input.TagsSet {
		item.Tags = append([]string(nil), input.Tags...)
	}
	if input.Status != nil {
		item.Status = defaultStatus(*input.Status)
	}
	if input.ClearExpiresAt {
		item.ExpiresAt = nil
	} else if input.ExpiresAt != nil {
		item.ExpiresAt = input.ExpiresAt
	}
	if input.DecayPolicy != nil {
		item.DecayPolicy = strings.TrimSpace(*input.DecayPolicy)
	}
	if input.MetadataSet {
		item.Metadata = cloneAnyMap(input.Metadata)
	}
	item.UpdatedAt = time.Now().UTC()
	doc.Items[idx] = item
	if err := s.writeDocument(ctx, doc); err != nil {
		return MemoryItem{}, err
	}
	return item, nil
}

// ArchiveItem marks an item archived without deleting its Markdown body.
func (s *Store) ArchiveItem(ctx context.Context, id string) (MemoryItem, error) {
	status := MemoryItemStatusArchived
	return s.UpdateItem(ctx, id, MemoryItemUpdateInput{Status: &status})
}

// RestoreItem marks an archived/expired item active again.
func (s *Store) RestoreItem(ctx context.Context, id string) (MemoryItem, error) {
	status := MemoryItemStatusActive
	return s.UpdateItem(ctx, id, MemoryItemUpdateInput{Status: &status, ClearExpiresAt: true})
}

// DeleteItem archives by default and only removes item markup when force=true.
func (s *Store) DeleteItem(ctx context.Context, id string, force bool) (MemoryItem, error) {
	if !force {
		return s.ArchiveItem(ctx, id)
	}
	item, doc, idx, _, err := s.findItem(id)
	if err != nil {
		return MemoryItem{}, err
	}
	if strings.HasPrefix(item.ID, "legacy_") {
		return MemoryItem{}, errors.New("legacy memory items cannot be force deleted")
	}
	doc.Items = append(doc.Items[:idx], doc.Items[idx+1:]...)
	if err := s.writeDocument(ctx, doc); err != nil {
		return MemoryItem{}, err
	}
	item.Status = MemoryItemStatusArchived
	item.UpdatedAt = time.Now().UTC()
	return item, nil
}

func (s *Store) readDocumentOrNew(path string, item MemoryItem) (MemoryDocument, error) {
	file, err := s.ReadFile(path)
	if err == nil {
		return ParseMemoryDocument(file), nil
	}
	if !os.IsNotExist(err) {
		return MemoryDocument{}, err
	}
	frontmatter := map[string]string{
		"scope":      string(defaultScope(item.Scope)),
		"type":       string(defaultType(item.Type)),
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	title := "Memory"
	switch item.Scope {
	case MemoryScopeProject:
		title = "Project Memory"
	case MemoryScopeProcedure:
		title = "Procedure Memory"
	default:
		if item.Type == MemoryTypePreference {
			title = "User Preferences"
		} else {
			title = "Long Term Memory"
		}
	}
	return MemoryDocument{
		Path:        path,
		Frontmatter: frontmatter,
		Preamble:    "# " + title,
		Items:       []MemoryItem{},
	}, nil
}

func (s *Store) writeDocument(ctx context.Context, doc MemoryDocument) error {
	doc.Frontmatter["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	file := RenderMemoryDocument(doc)
	if err := s.WriteFile(file); err != nil {
		return err
	}
	return s.Reindex(ctx)
}

func (s *Store) findItem(id string) (MemoryItem, MemoryDocument, int, string, error) {
	paths, err := s.ListFiles()
	if err != nil {
		return MemoryItem{}, MemoryDocument{}, -1, "", err
	}
	for _, path := range paths {
		file, err := s.ReadFile(path)
		if err != nil {
			return MemoryItem{}, MemoryDocument{}, -1, "", err
		}
		doc := ParseMemoryDocument(file)
		for idx, item := range doc.Items {
			if item.ID == id {
				item.Path = path
				doc.Path = path
				return item, doc, idx, path, nil
			}
		}
	}
	return MemoryItem{}, MemoryDocument{}, -1, "", fmt.Errorf("memory item %s not found", id)
}

func memoryItemMatchesFilter(item MemoryItem, filter MemoryItemFilter, now time.Time) bool {
	if filter.Scope != "" && item.Scope != filter.Scope {
		return false
	}
	if filter.Type != "" && item.Type != filter.Type {
		return false
	}
	if filter.ProjectKey != "" && item.ProjectKey != filter.ProjectKey {
		return false
	}
	if filter.Status != "" {
		if filter.Status == MemoryItemStatusExpired {
			if !itemExpired(item, now) && item.Status != MemoryItemStatusExpired {
				return false
			}
		} else if item.Status != filter.Status {
			return false
		}
	} else if !filter.IncludeArchived {
		if item.Status == MemoryItemStatusArchived || item.Status == MemoryItemStatusRejected || item.Status == MemoryItemStatusExpired {
			return false
		}
	}
	if !filter.IncludeArchived && itemExpired(item, now) && filter.Status != MemoryItemStatusExpired {
		return false
	}
	if filter.Tag != "" && !containsString(item.Tags, filter.Tag) {
		return false
	}
	if filter.Query != "" {
		query := strings.ToLower(filter.Query)
		haystack := strings.ToLower(item.Text + " " + strings.Join(item.Tags, " ") + " " + item.ProjectKey)
		if !strings.Contains(haystack, query) {
			return false
		}
	}
	return true
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// CreateReview queues a candidate for user review.
func (s *Store) CreateReview(candidate MemoryCandidate) (MemoryReviewItem, error) {
	scan := ScanSensitiveMemory(candidate.Text)
	candidate.Sensitive = candidate.Sensitive || scan.Sensitive
	candidate.Text = strings.TrimSpace(scan.Redacted)
	if candidate.Text == "" {
		return MemoryReviewItem{}, errors.New("candidate text is required")
	}
	if candidate.ID == "" {
		candidate.ID = ids.New("memcand")
	}
	if candidate.TargetPath == "" {
		candidate.TargetPath = memoryItemPath(MemoryItem{Scope: candidate.Scope, Type: candidate.Type, ProjectKey: candidate.ProjectKey})
	}
	if candidate.Sensitive {
		candidate.Text = scan.Redacted
	}
	conflicts, err := s.DetectConflicts(candidate)
	if err != nil {
		return MemoryReviewItem{}, err
	}
	conflictIDs := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		conflictIDs = append(conflictIDs, conflict.ConflictID)
	}
	status := MemoryReviewPending
	if candidate.Sensitive {
		status = MemoryReviewRejected
	}
	item := MemoryReviewItem{
		ReviewID:    ids.New("memrev"),
		Candidate:   candidate,
		Status:      status,
		ConflictIDs: conflictIDs,
		Conflicts:   conflicts,
		CreatedAt:   time.Now().UTC(),
	}
	if candidate.Sensitive {
		now := time.Now().UTC()
		item.DecidedAt = &now
		item.DecisionNote = "sensitive memory candidate rejected by guard"
	}
	if err := s.writeReview(item); err != nil {
		return MemoryReviewItem{}, err
	}
	return item, nil
}

// ListReviews returns review queue items sorted newest first.
func (s *Store) ListReviews(status MemoryReviewStatus, limit int) ([]MemoryReviewItem, error) {
	root := filepath.Join(s.Root, "review")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []MemoryReviewItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := []MemoryReviewItem{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		item, err := s.readReview(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		if status != "" && item.Status != status {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// GetReview returns one review item.
func (s *Store) GetReview(reviewID string) (MemoryReviewItem, error) {
	return s.readReview(reviewID)
}

// ApproveReview applies a reviewed candidate to Markdown memory.
func (s *Store) ApproveReview(ctx context.Context, reviewID, note string) (MemoryReviewItem, error) {
	review, err := s.readReview(reviewID)
	if err != nil {
		return MemoryReviewItem{}, err
	}
	if review.Status != MemoryReviewPending {
		return MemoryReviewItem{}, fmt.Errorf("review %s is not pending", reviewID)
	}
	if review.Candidate.Sensitive {
		return MemoryReviewItem{}, errors.New("sensitive memory candidate cannot be approved")
	}
	scan := ScanSensitiveMemory(review.Candidate.Text)
	if scan.Sensitive {
		return MemoryReviewItem{}, fmt.Errorf("refusing to approve sensitive memory: %s", strings.Join(scan.Findings, ","))
	}
	item, err := s.CreateItem(ctx, MemoryItemCreateInput{
		Scope:           review.Candidate.Scope,
		Type:            review.Candidate.Type,
		ProjectKey:      review.Candidate.ProjectKey,
		Text:            review.Candidate.Text,
		Source:          review.Candidate.Source,
		SourceMessageID: review.Candidate.SourceID,
		Confidence:      review.Candidate.Confidence,
		Importance:      review.Candidate.Importance,
		Tags:            review.Candidate.Tags,
		ExpiresAt:       review.Candidate.ExpiresAt,
		DecayPolicy:     review.Candidate.DecayPolicy,
		Path:            review.Candidate.TargetPath,
	})
	if err != nil {
		return MemoryReviewItem{}, err
	}
	now := time.Now().UTC()
	review.Status = MemoryReviewApplied
	review.DecidedAt = &now
	review.DecisionNote = note
	review.AppliedItem = &item
	if err := s.writeReview(review); err != nil {
		return MemoryReviewItem{}, err
	}
	return review, nil
}

// RejectReview rejects a candidate without writing memory.
func (s *Store) RejectReview(reviewID, note string) (MemoryReviewItem, error) {
	review, err := s.readReview(reviewID)
	if err != nil {
		return MemoryReviewItem{}, err
	}
	if review.Status != MemoryReviewPending && review.Status != MemoryReviewRejected {
		return MemoryReviewItem{}, fmt.Errorf("review %s is not rejectable", reviewID)
	}
	now := time.Now().UTC()
	review.Status = MemoryReviewRejected
	review.DecidedAt = &now
	review.DecisionNote = note
	if err := s.writeReview(review); err != nil {
		return MemoryReviewItem{}, err
	}
	return review, nil
}

func (s *Store) writeReview(item MemoryReviewItem) error {
	if item.ReviewID == "" {
		return errors.New("review id is required")
	}
	root := filepath.Join(s.Root, "review")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	raw = []byte(security.RedactString(string(raw)))
	return os.WriteFile(filepath.Join(root, item.ReviewID+".json"), raw, 0o644)
}

func (s *Store) readReview(reviewID string) (MemoryReviewItem, error) {
	if strings.TrimSpace(reviewID) == "" || strings.Contains(reviewID, "/") || strings.Contains(reviewID, `\`) {
		return MemoryReviewItem{}, errors.New("invalid review id")
	}
	raw, err := os.ReadFile(filepath.Join(s.Root, "review", reviewID+".json"))
	if err != nil {
		return MemoryReviewItem{}, err
	}
	var item MemoryReviewItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return MemoryReviewItem{}, err
	}
	return item, nil
}

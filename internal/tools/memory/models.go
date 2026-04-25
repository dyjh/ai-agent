package memory

import "time"

// MemoryScope describes the ownership boundary for a memory item.
type MemoryScope string

const (
	MemoryScopeUser         MemoryScope = "user"
	MemoryScopeProject      MemoryScope = "project"
	MemoryScopeProcedure    MemoryScope = "procedure"
	MemoryScopeConversation MemoryScope = "conversation"
)

// MemoryType describes the semantic class of a memory item.
type MemoryType string

const (
	MemoryTypePreference MemoryType = "preference"
	MemoryTypeFact       MemoryType = "fact"
	MemoryTypeProcedure  MemoryType = "procedure"
	MemoryTypeProject    MemoryType = "project"
	MemoryTypeEpisode    MemoryType = "episode"
)

// MemoryItemStatus is the lifecycle state stored in Markdown item metadata.
type MemoryItemStatus string

const (
	MemoryItemStatusActive   MemoryItemStatus = "active"
	MemoryItemStatusPending  MemoryItemStatus = "pending"
	MemoryItemStatusRejected MemoryItemStatus = "rejected"
	MemoryItemStatusExpired  MemoryItemStatus = "expired"
	MemoryItemStatusArchived MemoryItemStatus = "archived"
)

// MemoryItem is the item-level abstraction rendered into Markdown files.
type MemoryItem struct {
	ID              string           `json:"id"`
	Scope           MemoryScope      `json:"scope"`
	Type            MemoryType       `json:"type"`
	ProjectKey      string           `json:"project_key,omitempty"`
	Text            string           `json:"text"`
	Source          string           `json:"source,omitempty"`
	SourceMessageID string           `json:"source_message_id,omitempty"`
	Confidence      float64          `json:"confidence"`
	Importance      float64          `json:"importance"`
	Tags            []string         `json:"tags,omitempty"`
	Sensitive       bool             `json:"sensitive"`
	Status          MemoryItemStatus `json:"status"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	ExpiresAt       *time.Time       `json:"expires_at,omitempty"`
	LastUsedAt      *time.Time       `json:"last_used_at,omitempty"`
	UseCount        int              `json:"use_count"`
	DecayPolicy     string           `json:"decay_policy,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
	Path            string           `json:"path,omitempty"`
}

// MemoryDocument is a parsed Markdown memory file.
type MemoryDocument struct {
	Path        string            `json:"path"`
	Frontmatter map[string]string `json:"frontmatter,omitempty"`
	Preamble    string            `json:"preamble,omitempty"`
	Items       []MemoryItem      `json:"items"`
}

// MemoryItemFilter controls item listing and context selection.
type MemoryItemFilter struct {
	Scope           MemoryScope
	Type            MemoryType
	ProjectKey      string
	Status          MemoryItemStatus
	Tag             string
	Query           string
	Limit           int
	IncludeArchived bool
}

// MemoryItemCreateInput is the write snapshot for memory.item_create.
type MemoryItemCreateInput struct {
	Scope           MemoryScope    `json:"scope"`
	Type            MemoryType     `json:"type"`
	ProjectKey      string         `json:"project_key,omitempty"`
	Text            string         `json:"text"`
	Source          string         `json:"source,omitempty"`
	SourceMessageID string         `json:"source_message_id,omitempty"`
	Confidence      float64        `json:"confidence,omitempty"`
	Importance      float64        `json:"importance,omitempty"`
	Tags            []string       `json:"tags,omitempty"`
	ExpiresAt       *time.Time     `json:"expires_at,omitempty"`
	DecayPolicy     string         `json:"decay_policy,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	Path            string         `json:"path,omitempty"`
}

// MemoryItemUpdateInput is the write snapshot for memory.item_update.
type MemoryItemUpdateInput struct {
	Text           *string           `json:"text,omitempty"`
	Scope          *MemoryScope      `json:"scope,omitempty"`
	Type           *MemoryType       `json:"type,omitempty"`
	ProjectKey     *string           `json:"project_key,omitempty"`
	Confidence     *float64          `json:"confidence,omitempty"`
	Importance     *float64          `json:"importance,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
	TagsSet        bool              `json:"tags_set,omitempty"`
	Status         *MemoryItemStatus `json:"status,omitempty"`
	ExpiresAt      *time.Time        `json:"expires_at,omitempty"`
	ClearExpiresAt bool              `json:"clear_expires_at,omitempty"`
	DecayPolicy    *string           `json:"decay_policy,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
	MetadataSet    bool              `json:"metadata_set,omitempty"`
}

// MemoryExtractInput is the source payload for memory candidate extraction.
type MemoryExtractInput struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id,omitempty"`
	Text           string `json:"text"`
	ProjectKey     string `json:"project_key,omitempty"`
}

// MemoryCandidate is a proposed memory item that has not been committed.
type MemoryCandidate struct {
	ID          string      `json:"id"`
	Scope       MemoryScope `json:"scope"`
	Type        MemoryType  `json:"type"`
	ProjectKey  string      `json:"project_key,omitempty"`
	Text        string      `json:"text"`
	Confidence  float64     `json:"confidence"`
	Importance  float64     `json:"importance"`
	Sensitive   bool        `json:"sensitive"`
	Reason      string      `json:"reason"`
	TargetPath  string      `json:"target_path"`
	Source      string      `json:"source,omitempty"`
	SourceID    string      `json:"source_id,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	ExpiresAt   *time.Time  `json:"expires_at,omitempty"`
	DecayPolicy string      `json:"decay_policy,omitempty"`
}

// MemoryReviewStatus tracks the candidate review lifecycle.
type MemoryReviewStatus string

const (
	MemoryReviewPending  MemoryReviewStatus = "pending"
	MemoryReviewApproved MemoryReviewStatus = "approved"
	MemoryReviewRejected MemoryReviewStatus = "rejected"
	MemoryReviewApplied  MemoryReviewStatus = "applied"
)

// MemoryReviewItem is a persisted review queue entry.
type MemoryReviewItem struct {
	ReviewID     string             `json:"review_id"`
	Candidate    MemoryCandidate    `json:"candidate"`
	Status       MemoryReviewStatus `json:"status"`
	ConflictIDs  []string           `json:"conflict_ids,omitempty"`
	Conflicts    []MemoryConflict   `json:"conflicts,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	DecidedAt    *time.Time         `json:"decided_at,omitempty"`
	DecisionNote string             `json:"decision_note,omitempty"`
	AppliedItem  *MemoryItem        `json:"applied_item,omitempty"`
}

// MemoryConflict describes a basic candidate-vs-existing memory conflict.
type MemoryConflict struct {
	ConflictID  string   `json:"conflict_id"`
	Type        string   `json:"type"`
	CandidateID string   `json:"candidate_id"`
	ExistingIDs []string `json:"existing_ids"`
	Summary     string   `json:"summary"`
	Severity    string   `json:"severity"`
}

// SensitiveMemoryScan is returned by the memory guard.
type SensitiveMemoryScan struct {
	Sensitive bool     `json:"sensitive"`
	Redacted  string   `json:"redacted"`
	Findings  []string `json:"findings,omitempty"`
}

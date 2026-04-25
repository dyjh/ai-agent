package kb

import (
	"context"
	"fmt"
	"log/slog"

	"local-agent/internal/config"
)

// VectorIndexFactory builds vector indexes from config.
type VectorIndexFactory interface {
	NewVectorIndex(ctx context.Context, cfg config.Config, embedder Embedder) (VectorIndex, error)
}

// DefaultVectorIndexFactory selects between memory and remote vector backends.
type DefaultVectorIndexFactory struct {
	Logger *slog.Logger
}

// NewVectorIndexFactory constructs the default factory.
func NewVectorIndexFactory(logger *slog.Logger) *DefaultVectorIndexFactory {
	return &DefaultVectorIndexFactory{Logger: logger}
}

// NewVectorIndex builds the configured vector backend and applies fallback rules.
func (f *DefaultVectorIndexFactory) NewVectorIndex(ctx context.Context, cfg config.Config, embedder Embedder) (VectorIndex, error) {
	status := VectorRuntimeStatus{
		VectorBackend: string(cfg.Vector.Backend),
		Collections:   collectionStatus(cfg),
	}

	switch cfg.Vector.Backend {
	case "", config.VectorBackendMemory:
		status.VectorBackend = string(config.VectorBackendMemory)
		return NewInMemoryVectorIndex(embedder, status), nil
	case config.VectorBackendQdrant:
		index, err := NewQdrantVectorIndex(QdrantIndexConfig{
			URL:                         cfg.Qdrant.URL,
			APIKey:                      cfg.Qdrant.APIKey,
			TimeoutSeconds:              cfg.Qdrant.TimeoutSeconds,
			EmbeddingDimension:          cfg.Vector.EmbeddingDimension,
			Distance:                    cfg.Vector.Distance,
			RecreateOnDimensionMismatch: cfg.Qdrant.RecreateOnDimensionMismatch,
			Collections:                 configuredCollections(cfg),
		}, embedder, nil)
		if err == nil {
			err = index.Health(ctx)
		}
		if err == nil {
			err = index.EnsureCollections(ctx)
		}
		if err == nil {
			return index, nil
		}
		if isQdrantCollectionDimensionError(err) {
			return nil, err
		}
		if !cfg.Vector.FallbackToMemory {
			return nil, err
		}

		fallback := fmt.Sprintf("qdrant unavailable: %v", err)
		if f.Logger != nil {
			f.Logger.Warn("vector backend fallback", "backend", "qdrant", "fallback", "memory", "reason", fallback)
		}
		status.VectorBackend = string(config.VectorBackendMemory)
		status.FallbackReason = fallback
		status.Qdrant = "fallback"
		return NewInMemoryVectorIndex(embedder, status), nil
	case config.VectorBackendPinecone:
		index, err := NewPineconeVectorIndex(PineconeIndexConfig{
			IndexHost:          cfg.Pinecone.IndexHost,
			APIKey:             cfg.Pinecone.APIKey,
			TimeoutSeconds:     cfg.Pinecone.TimeoutSeconds,
			EmbeddingDimension: cfg.Vector.EmbeddingDimension,
			Namespaces:         configuredCollections(cfg),
		}, embedder, nil)
		if err == nil {
			err = index.EnsureCollections(ctx)
		}
		if err == nil {
			return index, nil
		}
		if !cfg.Vector.FallbackToMemory {
			return nil, err
		}
		fallback := fmt.Sprintf("pinecone unavailable: %v", err)
		if f.Logger != nil {
			f.Logger.Warn("vector backend fallback", "backend", "pinecone", "fallback", "memory", "reason", fallback)
		}
		status.VectorBackend = string(config.VectorBackendMemory)
		status.FallbackReason = fallback
		return NewInMemoryVectorIndex(embedder, status), nil
	case config.VectorBackendOpenAI:
		index, err := NewOpenAIVectorStoreIndex(OpenAIVectorStoreConfig{
			BaseURL:        cfg.OpenAIKB.BaseURL,
			APIKey:         cfg.OpenAIKB.APIKey,
			TimeoutSeconds: cfg.OpenAIKB.TimeoutSeconds,
			VectorStores:   configuredCollections(cfg),
		}, nil)
		if err == nil {
			err = index.EnsureCollections(ctx)
		}
		if err == nil {
			return index, nil
		}
		if !cfg.Vector.FallbackToMemory {
			return nil, err
		}
		fallback := fmt.Sprintf("openai vector store unavailable: %v", err)
		if f.Logger != nil {
			f.Logger.Warn("vector backend fallback", "backend", "openai", "fallback", "memory", "reason", fallback)
		}
		status.VectorBackend = string(config.VectorBackendMemory)
		status.FallbackReason = fallback
		return NewInMemoryVectorIndex(embedder, status), nil
	default:
		return nil, fmt.Errorf("unsupported vector backend: %s", cfg.Vector.Backend)
	}
}

// StatusFromIndex extracts runtime status from a concrete index when available.
func StatusFromIndex(index VectorIndex) VectorRuntimeStatus {
	if provider, ok := index.(interface{ Status() VectorRuntimeStatus }); ok {
		return provider.Status()
	}
	return VectorRuntimeStatus{}
}

func collectionStatus(cfg config.Config) map[string]string {
	collections := configuredCollections(cfg)
	status := make(map[string]string, len(collections))
	for _, name := range collections {
		status[name] = "configured"
	}
	return status
}

func configuredCollections(cfg config.Config) map[string]string {
	out := map[string]string{}
	for _, scope := range []string{"kb", "memory", "code"} {
		if name := cfg.CollectionName(scope); name != "" {
			out[scope] = name
		}
	}
	return out
}

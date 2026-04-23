package kb

import (
	"context"
	"hash/fnv"
)

// Embedder turns text into vectors.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// FakeEmbedder provides deterministic embeddings for tests and local MVP mode.
type FakeEmbedder struct {
	Dimensions int
}

// Embed creates a deterministic pseudo-embedding.
func (e FakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	dims := e.Dimensions
	if dims <= 0 {
		dims = 32
	}

	vector := make([]float32, dims)
	for index, token := range []byte(text) {
		hasher := fnv.New32a()
		_, _ = hasher.Write([]byte{token})
		slot := int(hasher.Sum32()) % dims
		vector[slot] += float32((index%7)+1) / 10
	}
	return vector, nil
}

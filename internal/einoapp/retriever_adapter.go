package einoapp

import "context"

// TextRetriever is the minimal retrieval abstraction used by the context builder.
type TextRetriever interface {
	Retrieve(ctx context.Context, query string, limit int) ([]string, error)
}

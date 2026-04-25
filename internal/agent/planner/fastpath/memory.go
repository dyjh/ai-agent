package fastpath

// Memory fast-path logic currently lives in deterministic.go because it needs
// the request conversation id and secret guard in the same branch.

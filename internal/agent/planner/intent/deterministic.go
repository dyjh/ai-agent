package intent

import "local-agent/internal/agent/planner/normalize"

// DeterministicClassifier is kept as an explicit name for tests and future
// wiring. It is the local, non-LLM classifier.
type DeterministicClassifier = Classifier

// ClassifyDeterministic classifies a request using local normalized signals.
func ClassifyDeterministic(req normalize.NormalizedRequest) IntentClassification {
	return New().Classify(req)
}

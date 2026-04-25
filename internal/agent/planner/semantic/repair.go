package semantic

// RepairPrompt asks the model to repair invalid JSON only. The repaired output
// still must pass local validation.
func RepairPrompt(previous string) string {
	return "Return only a valid SemanticPlan JSON object. Do not execute tools. Previous invalid response:\n" + previous
}

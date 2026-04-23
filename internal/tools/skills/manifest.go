package skills

// Manifest is the local skill descriptor used for validation.
type Manifest struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Entry       string `json:"entry" yaml:"entry"`
}

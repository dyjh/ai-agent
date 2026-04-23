package skills

// UploadInput is the payload for skill registration.
type UploadInput struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

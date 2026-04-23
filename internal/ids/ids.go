package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// New returns a prefixed sortable-ish identifier.
func New(prefix string) string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UTC().UnixMilli(), hex.EncodeToString(buf))
}

package common

import (
	"crypto/sha256"
	"fmt"
)

// PayloadMetadata returns bounded diagnostic metadata without exposing payload contents.
func PayloadMetadata(data []byte) string {
	digest := sha256.Sum256(data)
	return fmt.Sprintf("size=%d sha256=%x", len(data), digest)
}

// Package secrets resolves asset secrets. v1 reads them from environment variables.
package secrets

import (
	"fmt"
	"os"
)

// Env implements assets.Secrets with os.Getenv.
type Env struct{}

func (Env) Get(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("secret %s is not set (set the %s environment variable)", name, name)
	}
	return v, nil
}

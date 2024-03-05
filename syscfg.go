package syscfg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync/atomic"
	"time"
)

var (
	// versions embedded at build time.
	Version     = ""
	GitRevision = ""
)

// GetVersion returns the version embedded at build time.
func GetVersion() string {
	if Version == "" {
		return "custom"
	}
	return Version
}

// GetRevision returns the git revision embedded at build time.
func GetRevision() string {
	if GitRevision == "" {
		return "unknown"
	}
	return GitRevision
}

type Config struct {
	Logging LogConfig `json:"logging"`
}

func LoadConfig(path string) (*Config, error) {
	//nolint:gosec
	jsonBytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{}, nil
		}
		return &Config{}, err
	}

	newConfig := &Config{}
	if err = json.Unmarshal(jsonBytes, newConfig); err != nil {
		return &Config{}, err
	}

	return newConfig, nil
}

type ContextKey string
const HCReqKey = ContextKey("healthcheck")

// HealthySleep allows a process to sleep while stil responding to context cancellation AND healthchecks. Returns false if cancelled.
func HealthySleep(ctx context.Context, timeout time.Duration) bool {
	hc, ok := ctx.Value(HCReqKey).(*atomic.Bool)
	if !ok {
		// this should never happen, so avoiding having to pass a logger by just printing
		//nolint:forbidigo
		fmt.Println("context passed to HealthySleep without healthcheck value")
	}

	stop := &atomic.Bool{}
	defer stop.Store(true)

	go func() {
		for {
			if hc.Swap(false) {
				//nolint:forbidigo
				fmt.Println("HEALTHY")
			}
			if stop.Load() {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(timeout):
			return true
		}
	}
}

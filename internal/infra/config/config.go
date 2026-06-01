// Package config loads and saves user-defined exclusion rules as JSON.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
)

// DefaultPath is exclusion-rules.json next to the executable, falling back to
// ./exclusion-rules.json when the executable path can't be resolved.
func DefaultPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "./exclusion-rules.json"
	}
	return filepath.Join(filepath.Dir(exe), "exclusion-rules.json")
}

// Load reads and validates rule specs from path. A missing file is seeded with
// DefaultRuleSpecs(), saved, and returned. A malformed file or invalid spec
// returns a descriptive error naming the path; it never silently falls back.
func Load(path string) ([]transaction.RuleSpec, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		specs := transaction.DefaultRuleSpecs()
		if err := Save(path, specs); err != nil {
			return nil, err
		}
		return specs, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var specs []transaction.RuleSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for i, s := range specs {
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("%s: rule %d: %w", path, i+1, err)
		}
	}
	return specs, nil
}

// Save writes specs to path as indented JSON.
func Save(path string, specs []transaction.RuleSpec) error {
	data, err := json.MarshalIndent(specs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// Package config loads and saves user-defined exclusion rules as JSON.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
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

// File is the on-disk configuration: exclusion rules plus optional VISA
// reconciliation settings. It is stored as a JSON object.
type File struct {
	Exclusions    []transaction.RuleSpec       `json:"exclusions"`
	VisaReconcile *transaction.ReconcileConfig `json:"visaReconcile,omitempty"`
}

// Load reads and validates the config object from path. A missing file is
// seeded with DefaultRuleSpecs() and DefaultReconcileConfig(), saved, and
// returned. A malformed file or invalid entry returns a descriptive error
// naming the path; it never silently falls back.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		f := File{
			Exclusions:    transaction.DefaultRuleSpecs(),
			VisaReconcile: transaction.DefaultReconcileConfig(),
		}
		if err := Save(path, f); err != nil {
			return File{}, err
		}
		return f, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	for i, s := range f.Exclusions {
		if err := s.Validate(); err != nil {
			return File{}, fmt.Errorf("%s: rule %d: %w", path, i+1, err)
		}
	}
	if f.VisaReconcile != nil {
		if err := f.VisaReconcile.Validate(); err != nil {
			return File{}, fmt.Errorf("%s: visaReconcile: %w", path, err)
		}
	}
	return f, nil
}

// Save writes the config object to path as indented JSON.
func Save(path string, f File) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

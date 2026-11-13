package sync_test

import (
	"errors"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/sync"
)

func TestOptionsValidateRejectsContradictions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		options sync.Options
		want    string
	}{
		{"check and locked", sync.Options{Check: true, Locked: true}, "--check and --locked cannot be combined"},
		{"check and clean", sync.Options{Check: true, Clean: true}, "--check and --clean cannot be combined"},
		{"check and force", sync.Options{Check: true, Force: true}, "--check and --force cannot be combined"},
		{"locked and clean", sync.Options{Locked: true, Clean: true}, "--locked and --clean cannot be combined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.options.Validate()
			detail, ok := errors.AsType[*errs.Error](err)
			if !ok || detail.Code != errs.Usage || err.Error() != tt.want {
				t.Fatalf("Validate() error = %v, want a usage error %q", err, tt.want)
			}
			if detail.Fields[errs.LegacyCodeField] != errs.LegacyInvalidArgs || detail.Hint == "" {
				t.Errorf("Validate() error = %#v, want the legacy code and a hint", detail)
			}
		})
	}
}

func TestOptionsValidateAcceptsCompatibleOptions(t *testing.T) {
	t.Parallel()
	for _, options := range []sync.Options{
		{},
		{Check: true},
		{Locked: true},
		{Force: true, Clean: true},
		{Force: true, Locked: true, DryRun: true},
	} {
		if err := options.Validate(); err != nil {
			t.Errorf("Validate(%+v) error = %v, want nil", options, err)
		}
	}
}

func TestRunRejectsContradictoryOptionsBeforeDoingWork(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{ConfigDir: t.TempDir(), Output: ".dox"}
	_, err := sync.Run(t.Context(), cfg, sync.Options{Check: true, Force: true})
	if detail, ok := errors.AsType[*errs.Error](err); !ok || detail.Code != errs.Usage {
		t.Errorf("Run() error = %v, want a usage error", err)
	}
}

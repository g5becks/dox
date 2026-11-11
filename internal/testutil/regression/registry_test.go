package regression_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/testutil/regression"
)

func TestClassification(t *testing.T) {
	t.Parallel()
	failure := errors.New("actual regression")
	registry := map[string]string{"known": "fixing issue"}
	for _, tc := range []struct {
		name      string
		id        string
		failure   error
		wantError bool
	}{
		{"known failure accepted", "known", failure, false},
		{"known pass rejected", "known", nil, true},
		{"new failure rejected", "new", failure, true},
		{"new pass accepted", "new", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := regression.Classify(registry, tc.id, tc.failure)
			if (err != nil) != tc.wantError {
				t.Fatalf("classification = %v, wantError %t", err, tc.wantError)
			}
			if tc.id == "known" && tc.failure == nil && !strings.Contains(err.Error(), "remove from knownFailing") {
				t.Fatalf("missing removal instruction: %v", err)
			}
			if tc.id == "new" && tc.failure != nil && !errors.Is(err, failure) {
				t.Fatalf("lost original error: %v", err)
			}
		})
	}
}

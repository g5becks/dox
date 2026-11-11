// Package jsongolden canonicalizes JSON for order-insensitive object goldens.
package jsongolden

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Canonical preserves array order and exact numbers while sorting object keys.
func Canonical(input []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("trailing JSON: %w", err)
		}
		return nil, errors.New("multiple JSON documents")
	}
	return json.MarshalIndent(document, "", "  ")
}

package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/errs"
)

type catPosition struct {
	path   string
	offset int
}

func resolveCatPosition(cmd *cli.Command) (catPosition, error) {
	position := catPosition{path: cmd.Args().Get(1)}
	line := cmd.Int(flagLine)
	hasSuffix := false
	if colon := strings.LastIndex(position.path, ":"); colon >= 0 && decimalSuffix(position.path[colon+1:]) {
		parsed, parseErr := strconv.Atoi(position.path[colon+1:])
		if parseErr != nil {
			return position, catPositionError("line number is out of range")
		}
		line = parsed
		position.path = position.path[:colon]
		hasSuffix = true
	}
	locations := 0
	for _, specified := range []bool{cmd.IsSet(flagLine), cmd.IsSet("offset"), hasSuffix} {
		if specified {
			locations++
		}
	}
	if locations > 1 {
		return position, catPositionError("specify only one location: --line, path:line, or --offset")
	}
	if position.path == "" {
		return position, catPositionError("file path must not be empty")
	}
	if cmd.IsSet("offset") {
		offset := cmd.Int("offset")
		if offset < 0 || offset == math.MaxInt {
			return position, catPositionError("offset must be nonnegative and support a 1-based line")
		}
		position.offset = offset
		_, _ = fmt.Fprintln(
			os.Stderr,
			"warning: --offset is deprecated and will be removed in v0.6.0; use 1-based --line",
		)
		return position, nil
	}
	if line < 1 {
		return position, catPositionError("line must be a positive 1-based number")
	}
	position.offset = line - 1
	return position, nil
}

func decimalSuffix(text string) bool {
	text = strings.TrimPrefix(strings.TrimPrefix(text, "+"), "-")
	if text == "" {
		return false
	}
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func catPositionError(message string) error {
	return errs.New(
		errs.Usage,
		message,
		errs.WithHint("Use --line N or file:N for 1-based locations; --offset is deprecated"),
	)
}

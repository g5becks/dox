package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/errs"
)

const (
	exitOK             = 0
	exitRuntimeNetwork = 1
	exitUsageConfig    = 2
	exitPartialSync    = 3
	exitNotFound       = 4
	exitInterrupted    = 130
)

const rootDescription = `Exit codes:
0    ok
1    runtime/network
2    usage/config
3    partial sync failure
4    not found
130  interrupted`

type jsonErrorEnvelope struct {
	Error jsonErrorBody `json:"error"`
}

type jsonErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Hint    string         `json:"hint"`
	Fields  map[string]any `json:"fields"`
}

func handleUsageError(_ context.Context, cmd *cli.Command, err error, _ bool) error {
	return errs.New(
		errs.Usage,
		err.Error(),
		errs.WithHint(fmt.Sprintf("Run '%s --help' for usage", cmd.FullName())),
		errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
		errs.WithCause(err),
	)
}

func attachUsageHandlers(root *cli.Command) {
	root.OnUsageError = handleUsageError
	root.ExitErrHandler = func(context.Context, *cli.Command, error) {}
	for _, sub := range root.Commands {
		sub.OnUsageError = handleUsageError
	}
}

func jsonEnabled(cmd *cli.Command) bool {
	return cmd.Bool(formatJSON) || cmd.Root().Bool(formatJSON)
}

func verboseEnabled(cmd *cli.Command) bool {
	return cmd.Bool(flagVerbose) || cmd.Root().Bool(flagVerbose)
}

func renderError(w io.Writer, err error, args []string) int {
	if err == nil {
		return exitOK
	}

	detail := normalizeError(err)
	code := exitCodeForError(err, detail)

	if hasJSONArg(args) {
		fields := map[string]any{}
		if len(detail.Fields) > 0 {
			maps.Copy(fields, detail.Fields)
		}
		envelope := jsonErrorEnvelope{
			Error: jsonErrorBody{
				Code:    string(detail.Code),
				Message: detail.Msg,
				Hint:    detail.Hint,
				Fields:  fields,
			},
		}
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(envelope)
		return code
	}

	_, _ = fmt.Fprintln(w, detail.Msg)
	if detail.Hint != "" {
		_, _ = fmt.Fprintf(w, "hint: %s\n", detail.Hint)
	}
	if hasVerboseArg(args) {
		renderVerboseDetails(w, err, detail)
	}

	return code
}

func renderVerboseDetails(w io.Writer, err error, detail *errs.Error) {
	if len(detail.Fields) > 0 {
		keys := slices.Sorted(maps.Keys(detail.Fields))
		pairs := make([]string, 0, len(keys))
		for _, key := range keys {
			pairs = append(pairs, fmt.Sprintf("%s=%v", key, detail.Fields[key]))
		}
		_, _ = fmt.Fprintf(w, "fields: %s\n", strings.Join(pairs, " "))
	}

	prev := detail.Msg
	for cause := errors.Unwrap(err); cause != nil; cause = errors.Unwrap(cause) {
		msg := cause.Error()
		if msg != "" && msg != prev {
			_, _ = fmt.Fprintf(w, "cause: %s\n", msg)
			prev = msg
		}
	}
}

func normalizeError(err error) *errs.Error {
	if detail, ok := errors.AsType[*errs.Error](err); ok {
		if errors.Is(err, context.Canceled) && detail.Code != errs.Interrupted {
			copied := *detail
			copied.Code = errs.Interrupted
			return &copied
		}
		return detail
	}

	if errors.Is(err, context.Canceled) {
		return errs.Wrap(errs.Interrupted, err)
	}

	if exitCoder, ok := errors.AsType[cli.ExitCoder](err); ok {
		return errs.New(
			errs.Usage,
			exitCoder.Error(),
			errs.WithHint("Run 'dox --help' for usage"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
			errs.WithCause(err),
		)
	}

	return errs.Wrap(errs.Internal, err)
}

func exitCodeForError(err error, detail *errs.Error) int {
	if errors.Is(err, context.Canceled) || detail.Code == errs.Interrupted {
		return exitInterrupted
	}

	switch detail.Code {
	case errs.Usage, errs.Config:
		return exitUsageConfig
	case errs.PartialFailure, errs.Stale:
		return exitPartialSync
	case errs.NotFound:
		return exitNotFound
	case errs.Network, errs.RateLimited, errs.Internal, errs.Interrupted:
		return exitRuntimeNetwork
	default:
		return exitRuntimeNetwork
	}
}

func hasJSONArg(args []string) bool {
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--json" || arg == "--json=true" || arg == "--format=json" {
			return true
		}
		if arg == "--format" && i+1 < len(args) && args[i+1] == formatJSON {
			return true
		}
	}
	return false
}

func hasVerboseArg(args []string) bool {
	hasSubcommand := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") {
			hasSubcommand = true
		}
		if arg == "--verbose" || arg == "--verbose=true" {
			return true
		}
		if hasSubcommand && arg == "-v" {
			return true
		}
	}
	return false
}

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"encoding/json"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/search"
	"github.com/g5becks/dox/internal/testutil/regression"
)

type auditStateKey struct{}
type auditState struct {
	ctx      context.Context
	env      []string
	active   atomic.Int64
	maximum  atomic.Int64
	requests atomic.Int64
}

func TestAuditCLI(t *testing.T) {
	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir: "testdata/regression",
		Setup: func(env *testscript.Env) error {
			env.Values[auditStateKey{}] = &auditState{ctx: t.Context(), env: append([]string(nil), env.Vars...)}
			return nil
		},
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"audit":         scriptAudit,
			"envsubst":      scriptEnvSubst,
			"hangserve":     scriptHangServe,
			"parallelserve": scriptParallelServe,
			"manifest":      scriptManifest,
		},
		RequireUniqueNames: true,
	})
}

func scriptAudit(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: audit case-id operation [args...]")
	}
	id, operation := args[0], args[1]
	var failure error
	switch operation {
	case "refuse":
		failure = auditRefusal(ts, args[2:])
	case "invalid-config":
		if len(args) != 3 {
			ts.Fatalf("invalid-config needs a file")
		}
		if _, err := config.Load(ts.MkAbs(args[2])); err == nil {
			failure = errors.New("R1 accepted unsafe absolute root output; deletion deliberately not invoked")
		}
	case "success":
		if err := ts.Exec("dox", args[2:]...); err != nil {
			failure = fmt.Errorf("C3 command failed: %s: %w", ts.ReadFile("stderr"), err)
		} else {
			cfg, loadErr := config.Load(ts.MkAbs("dox.toml"))
			ts.Check(loadErr)
			added := cfg.Sources["x"]
			if added.Type != "github" || added.Repo != "acme/docs" || added.Path != "docs" {
				failure = fmt.Errorf("C3 source configuration mismatch: %+v", added)
			}
		}
	case "exit":
		failure = auditExit(ts, args[2:])
	case "parallel":
		ts.Check(ts.Exec("dox", "sync"))
		if maximum := ts.Value(auditStateKey{}).(*auditState).maximum.Load(); maximum != 12 {
			failure = fmt.Errorf("P2 concurrency = %d, want 12 from max_parallel=20", maximum)
		}
	case "timeout":
		failure = auditTimeout(ts)
	case "line":
		failure = auditLine(ts)
	default:
		ts.Fatalf("unknown audit operation %q", operation)
	}
	if err := regression.Verify(id, failure); err != nil {
		ts.Fatalf("%v", err)
	}
	if failure != nil {
		ts.Logf("KNOWN FAILING %s: %v", id, failure)
	}
}

func auditRefusal(ts *testscript.TestScript, args []string) error {
	if len(args) < 2 {
		ts.Fatalf("refuse needs sentinel and CLI arguments")
	}
	path := ts.MkAbs(args[0])
	before := ts.ReadFile(args[0])
	err := ts.Exec("dox", args[1:]...)
	after, readErr := os.ReadFile(path)
	intact := readErr == nil && string(after) == before
	if err == nil || !intact {
		return fmt.Errorf("R1 unsafe clean: succeeded=%t, sentinel intact=%t", err == nil, intact)
	}
	return nil
}

func auditExit(ts *testscript.TestScript, args []string) error {
	if len(args) < 2 {
		ts.Fatalf("exit needs status and CLI arguments")
	}
	want, parseErr := strconv.Atoi(args[0])
	ts.Check(parseErr)
	err := ts.Exec("dox", args[1:]...)
	exitCode := 0
	if err != nil {
		status, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			ts.Fatalf("command setup failure: %v", err)
		}
		exitCode = status.ExitCode()
	}
	if exitCode != want {
		return fmt.Errorf("C5 exit code = %d, want %d; stderr: %s", exitCode, want, ts.ReadFile("stderr"))
	}
	stderr := ts.ReadFile("stderr")
	if !strings.Contains(stderr, "hint:") || !strings.Contains(stderr, "dox list") {
		return fmt.Errorf("C5 missing hint: %s", stderr)
	}
	return nil
}

func auditTimeout(ts *testscript.TestScript) error {
	state := ts.Value(auditStateKey{}).(*auditState)
	const budget = 1250 * time.Millisecond // Configured 250ms plus one second.
	ctx, cancel := context.WithTimeout(state.ctx, budget)
	defer cancel()
	command := exec.CommandContext(ctx, "dox", "sync")
	command.Dir = ts.MkAbs(".")
	command.Env = state.env
	output, err := command.CombinedOutput()
	if state.requests.Load() == 0 {
		ts.Fatalf("timeout probe never reached the local server: %s, %v", output, err)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("R2 request exceeded configured timeout + 1s: %w", ctx.Err())
	}
	if err == nil ||
		(!strings.Contains(strings.ToLower(string(output)), "timeout") && !strings.Contains(string(output), "deadline")) {
		ts.Fatalf("unexpected timeout probe outcome: %s, %v", output, err)
	}
	return nil
}

func auditLine(ts *testscript.TestScript) error {
	ts.Check(ts.Exec("dox", "search", "needle", "--content", "--json"))
	var hits []search.ContentResult
	ts.Check(json.Unmarshal([]byte(ts.ReadFile("stdout")), &hits))
	if len(hits) != 1 {
		ts.Fatalf("expected one search hit, got %d", len(hits))
	}
	err := ts.Exec("dox", "cat", "docs", "guide.txt", "--line", strconv.Itoa(hits[0].Line), "--limit", "1")
	if err != nil {
		return fmt.Errorf("C6 cat failed: %s: %w", ts.ReadFile("stderr"), err)
	}
	if strings.TrimSpace(ts.ReadFile("stdout")) != "needle" {
		return fmt.Errorf("C6 line mismatch: %s", ts.ReadFile("stdout"))
	}
	return nil
}

func scriptHangServe(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: hangserve")
	}
	state := ts.Value(auditStateKey{}).(*auditState)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		state.requests.Add(1)
		<-r.Context().Done()
	}))
	ts.Setenv("SERVER", server.URL)
	ts.Defer(server.Close)
}

func scriptParallelServe(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: parallelserve")
	}
	state := ts.Value(auditStateKey{}).(*auditState)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		active := state.active.Add(1)
		defer state.active.Add(-1)
		for previous := state.maximum.Load(); active > previous; previous = state.maximum.Load() {
			if state.maximum.CompareAndSwap(previous, active) {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte("fixture\n"))
	}))
	ts.Defer(server.Close)
	var text strings.Builder
	text.WriteString("output = \".dox\"\nmax_parallel = 20\n")
	for index := range 12 {
		fmt.Fprintf(&text, "[sources.s%d]\ntype = \"url\"\nurl = %q\nfilename = \"guide.txt\"\n", index, server.URL)
	}
	ts.Check(os.WriteFile(ts.MkAbs("dox.toml"), []byte(text.String()), 0o600))
}

func scriptManifest(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: manifest")
	}
	cfg, err := config.Load(ts.MkAbs("dox.toml"))
	ts.Check(err)
	ts.Check(manifest.Generate(ts.Value(auditStateKey{}).(*auditState).ctx, cfg, nil))
}

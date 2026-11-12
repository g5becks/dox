package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/g5becks/dox/internal/testutil/jsongolden"
)

type scriptEnvKey struct{}

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"dox": main})
}

func TestScripts(t *testing.T) {
	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(env *testscript.Env) error {
			env.Values[scriptEnvKey{}] = append([]string(nil), env.Vars...)
			return nil
		},
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"httpserve":    scriptHTTPServe,
			"httpfail":     scriptHTTPFail,
			"envsubst":     scriptEnvSubst,
			"pathenv":      scriptPathEnv,
			"jsoncmp":      scriptJSONCompare,
			"doxexit":      scriptDoxExit,
			"doxinterrupt": scriptDoxInterrupt,
		},
		RequireUniqueNames: true,
	})
}

func scriptHTTPServe(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: httpserve file")
	}
	body := []byte(ts.ReadFile(args[0]))
	path := "/" + filepath.Base(args[0])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	ts.Defer(server.Close)
	ts.Setenv("SERVER", server.URL)
}

func scriptEnvSubst(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: envsubst input output")
	}
	text := os.Expand(ts.ReadFile(args[0]), ts.Getenv)
	ts.Check(os.WriteFile(ts.MkAbs(args[1]), []byte(text), 0o600))
}

func scriptPathEnv(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: pathenv key path-components...")
	}
	ts.Setenv(args[0], filepath.Join(args[1:]...))
}

func scriptJSONCompare(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: jsoncmp actual expected")
	}
	actual, err := jsongolden.Canonical([]byte(ts.ReadFile(args[0])))
	ts.Check(err)
	// Placeholders occur inside JSON strings; escape Windows paths correctly.
	wantText := os.Expand(ts.ReadFile(args[1]), func(key string) string {
		quoted, quoteErr := json.Marshal(ts.Getenv(key))
		ts.Check(quoteErr)
		return string(quoted[1 : len(quoted)-1])
	})
	want, err := jsongolden.Canonical([]byte(wantText))
	ts.Check(err)
	if !bytes.Equal(actual, want) {
		ts.Fatalf("JSON differs\ngot:\n%s\nwant:\n%s", actual, want)
	}
}

func scriptHTTPFail(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: httpfail status-code")
	}
	code, err := strconv.Atoi(args[0])
	ts.Check(err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}))
	ts.Defer(server.Close)
	ts.Setenv("FAIL_SERVER", server.URL)
}

func scriptDoxExit(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 1 {
		ts.Fatalf("usage: doxexit status-code [args...]")
	}
	want, err := strconv.Atoi(args[0])
	ts.Check(err)
	runErr := ts.Exec("dox", args[1:]...)
	got := 0
	if runErr != nil {
		exitErr, ok := errors.AsType[*exec.ExitError](runErr)
		if !ok {
			ts.Fatalf("unexpected execution error: %v", runErr)
		}
		got = exitErr.ExitCode()
	}
	if got != want {
		ts.Fatalf("exit code = %d, want %d\nstderr:\n%s", got, want, ts.ReadFile("stderr"))
	}
}

func scriptDoxInterrupt(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: doxinterrupt status-code args...")
	}
	want, err := strconv.Atoi(args[0])
	ts.Check(err)

	fastDone := make(chan struct{})
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fast.txt" {
			_, _ = w.Write([]byte("fast doc\n"))
			select {
			case <-fastDone:
			default:
				close(fastDone)
			}
			return
		}
		<-fastDone
		// Give the client a brief moment to finish writing fast.txt before signaling SIGINT.
		time.Sleep(50 * time.Millisecond)
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	configText := "output = \".dox\"\n" +
		"[sources.fast]\ntype = \"url\"\nurl = \"" + server.URL + "/fast.txt\"\nfilename = \"fast.txt\"\n" +
		"[sources.slow]\ntype = \"url\"\nurl = \"" + server.URL + "/slow.txt\"\nfilename = \"slow.txt\"\n"
	ts.Check(os.WriteFile(ts.MkAbs("dox.toml"), []byte(configText), 0o600))

	command := exec.Command("dox", args[1:]...)
	command.Dir = ts.MkAbs(".")
	baseEnv, _ := ts.Value(scriptEnvKey{}).([]string)
	command.Env = append(append([]string(nil), baseEnv...), "PATH="+ts.Getenv("PATH"))
	var stderr bytes.Buffer
	command.Stderr = &stderr
	ts.Check(command.Start())

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		ts.Fatalf("timed out waiting for dox to contact interrupt server")
	}

	ts.Check(command.Process.Signal(os.Interrupt))
	waitErr := command.Wait()
	exitErr, ok := errors.AsType[*exec.ExitError](waitErr)
	if !ok {
		ts.Fatalf("expected ExitError after interrupt, got %v (stderr: %s)", waitErr, stderr.String())
	}
	if exitErr.ExitCode() != want {
		ts.Fatalf("interrupt exit code = %d, want %d\nstderr:\n%s", exitErr.ExitCode(), want, stderr.String())
	}
	ts.Check(os.WriteFile(ts.MkAbs("interrupt.stderr"), stderr.Bytes(), 0o600))
}

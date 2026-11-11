package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/g5becks/dox/internal/testutil/jsongolden"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"dox": main})
}

func TestScripts(t *testing.T) {
	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"httpserve": scriptHTTPServe,
			"envsubst":  scriptEnvSubst,
			"pathenv":   scriptPathEnv,
			"jsoncmp":   scriptJSONCompare,
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

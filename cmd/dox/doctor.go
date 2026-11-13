package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lockfile"
	doxsync "github.com/g5becks/dox/internal/sync"
)

const (
	checkOK   = "ok"
	checkWarn = "warn"
	checkFail = "fail"

	githubRateLimitURL = "https://api.github.com/rate_limit"
	doctorHTTPTimeout  = 10 * time.Second
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

func newDoctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Check config, tokens, rate limits, output directory and lock consistency",
		Flags: append(formatFlags(),
			&cli.BoolFlag{Name: "offline", Usage: "Skip checks that need the network (GitHub rate limit)"},
		),
		Action: doctorAction,
	}
}

func doctorAction(ctx context.Context, cmd *cli.Command) error {
	format, err := resolveOutputFormat(cmd, nil)
	if err != nil {
		return err
	}
	report := runDoctor(ctx, cmd, githubRateLimitURL)
	if writeErr := writeDoctorReport(format, report); writeErr != nil {
		return writeErr
	}
	failures := 0
	for _, check := range report.Checks {
		if check.Status == checkFail {
			failures++
		}
	}
	if failures == 0 {
		return nil
	}
	return errs.New(
		errs.Internal,
		fmt.Sprintf("doctor found %d problem(s)", failures),
		errs.WithHint("Fix the checks marked fail above"),
	)
}

func runDoctor(ctx context.Context, cmd *cli.Command, rateLimitURL string) doctorReport {
	report := doctorReport{}
	add := func(name, status, detail string) {
		report.Checks = append(report.Checks, doctorCheck{Name: name, Status: status, Detail: detail})
	}
	configPath, err := resolveConfigPath(cmd.String(flagConfig))
	var cfg *config.Config
	if err == nil {
		cfg, err = loadCommandConfig(cmd, configPath)
	}
	if err != nil {
		add("config", checkFail, err.Error())
		return finishReport(report)
	}
	add("config", checkOK, fmt.Sprintf("%s (%d source(s))", configPath, len(cfg.Sources)))
	checkOutput(cfg, add)
	githubToken := checkTokens(cfg, add)
	if cmd.Bool("offline") {
		add("github rate limit", checkOK, "skipped (--offline)")
	} else if usesGitHubDotCom(cfg) {
		checkRateLimit(ctx, rateLimitURL, githubToken, add)
	}
	checkLock(cfg, add)
	return finishReport(report)
}

func finishReport(report doctorReport) doctorReport {
	report.OK = !slices.ContainsFunc(report.Checks, func(check doctorCheck) bool { return check.Status == checkFail })
	return report
}

func checkOutput(cfg *config.Config, add func(name, status, detail string)) {
	paths, err := cfg.Paths()
	if err != nil {
		add("output", checkFail, err.Error())
		return
	}
	if _, statErr := os.Stat(paths.Output()); errors.Is(statErr, os.ErrNotExist) {
		add("output", checkWarn, paths.Output()+" does not exist yet; run 'dox sync'")
		return
	}
	root, err := paths.OpenOutput(false)
	if err != nil {
		add("output", checkFail, err.Error())
		return
	}
	defer root.Close()
	if ownedErr := config.RequireOwned(root); ownedErr != nil {
		add("output", checkWarn, paths.Output()+" is not marked as dox output; run 'dox sync' once")
		return
	}
	add("output", checkOK, paths.Output())
}

// checkTokens reports, per host, whether a token is configured (never its
// value), and returns the github.com token for the rate-limit check.
func checkTokens(cfg *config.Config, add func(name, status, detail string)) string {
	hosts := map[string]config.Source{}
	for _, sourceCfg := range cfg.Sources {
		if sourceCfg.Repo != "" {
			hosts[strings.ToLower(sourceCfg.Host)] = sourceCfg
		}
	}
	githubToken := ""
	for _, host := range slices.Sorted(maps.Keys(hosts)) {
		token := doxsync.ResolveToken(cfg, hosts[host])
		if host == "github.com" || host == "" {
			githubToken = token
		}
		if token != "" {
			add("token "+host, checkOK, "configured")
			continue
		}
		add("token "+host, checkWarn, "none: public repositories only, at unauthenticated rate limits")
	}
	return githubToken
}

func usesGitHubDotCom(cfg *config.Config) bool {
	for _, sourceCfg := range cfg.Sources {
		if sourceCfg.Type == "github" && strings.EqualFold(sourceCfg.Host, "github.com") {
			return true
		}
	}
	return false
}

// checkRateLimit reads github.com's rate_limit endpoint, which does not count
// against the quota.
func checkRateLimit(ctx context.Context, url, token string, add func(name, status, detail string)) {
	ctx, cancel := context.WithTimeout(ctx, doctorHTTPTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		add("github rate limit", checkWarn, err.Error())
		return
	}
	request.Header.Set("User-Agent", "dox")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		add("github rate limit", checkWarn, "unreachable: "+err.Error())
		return
	}
	defer response.Body.Close()
	var body struct {
		Resources struct {
			Core struct {
				Limit     int   `json:"limit"`
				Remaining int   `json:"remaining"`
				Reset     int64 `json:"reset"`
			} `json:"core"`
		} `json:"resources"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&body) != nil {
		add("github rate limit", checkWarn, fmt.Sprintf("unexpected response (status %d)", response.StatusCode))
		return
	}
	core := body.Resources.Core
	detail := fmt.Sprintf("%d of %d requests remaining (resets %s)",
		core.Remaining, core.Limit, time.Unix(core.Reset, 0).UTC().Format(time.RFC3339))
	status := checkOK
	if core.Remaining < rateLimitWarnThreshold {
		status = checkWarn
	}
	add("github rate limit", status, detail)
}

// rateLimitWarnThreshold leaves room for a few syncs (2 calls per folder).
const rateLimitWarnThreshold = 10

// checkLock compares the lock with the config and the files on disk.
func checkLock(cfg *config.Config, add func(name, status, detail string)) {
	paths, err := cfg.Paths()
	if err != nil {
		return
	}
	lock, err := lockfile.Load(paths.Output())
	if err != nil {
		add("lock", checkFail, err.Error())
		return
	}
	var problems []string
	missingFiles := 0
	for _, name := range slices.Sorted(maps.Keys(cfg.Sources)) {
		sourceCfg := cfg.Sources[name]
		entry := lock.GetEntry(name)
		switch {
		case entry == nil:
			problems = append(problems, name+": not synced")
			continue
		case entry.ConfigDigest != "" && entry.ConfigDigest != sourceCfg.Digest():
			problems = append(problems, name+": config changed since the last sync")
		}
		directory, dirErr := paths.Source(name, sourceCfg)
		if dirErr != nil {
			continue
		}
		for file := range entry.Files {
			if _, statErr := os.Stat(filepath.Join(directory, filepath.FromSlash(file))); statErr != nil {
				missingFiles++
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(lock.Sources)) {
		if _, configured := cfg.Sources[name]; !configured {
			problems = append(problems, name+": in the lock but not in the config")
		}
	}
	switch {
	case missingFiles > 0:
		add("lock", checkFail, fmt.Sprintf("%d locked file(s) missing on disk; run 'dox sync --force'", missingFiles))
	case len(problems) > 0:
		add("lock", checkWarn, strings.Join(problems, "; "))
	default:
		add("lock", checkOK, fmt.Sprintf("%d source(s) consistent", len(lock.Sources)))
	}
}

func writeDoctorReport(format string, report doctorReport) error {
	switch format {
	case formatJSON:
		return writeJSONValue(report)
	case formatJSONL:
		return writeJSONLines(report.Checks)
	case formatCSV:
		rows := make([][]string, 0, len(report.Checks))
		for _, check := range report.Checks {
			rows = append(rows, []string{check.Name, check.Status, check.Detail})
		}
		return writeCSVRows([]string{"name", "status", "detail"}, rows)
	default:
		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.SetStyle(table.StyleRounded)
		t.AppendHeader(table.Row{"STATUS", "CHECK", "DETAIL"})
		for _, check := range report.Checks {
			t.AppendRow(table.Row{check.Status, check.Name, check.Detail})
		}
		t.Render()
		return nil
	}
}

// husky is the command-line interface for the Husky background scheduler.
//
// All commands communicate with the running daemon over a Unix socket.
// If no daemon is running, commands that require it will exit with a clear
// error message.
//
// Usage:
//
//	husky <command> [flags]
//
// Run `husky help` or `husky <command> --help` for detailed usage.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	daemoncmd "github.com/husky-scheduler/husky/cmd/huskyd"
	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/daemoncfg"
	"github.com/husky-scheduler/husky/internal/dag"
	"github.com/husky-scheduler/husky/internal/ipc"
	"github.com/husky-scheduler/husky/internal/notify"
	"github.com/husky-scheduler/husky/internal/store"
	"github.com/husky-scheduler/husky/internal/version"
)

// dataDir and configPath are set via persistent root flags and shared across
// all sub-commands.
var (
	dataDir    string
	configPath string
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "husky",
		Short: "Local-first job scheduler with dependency graphs",
		Long: `Husky is a local-first job scheduler that replaces cron for modern
development workflows. Jobs are defined in husky.yaml and executed as a
directed acyclic graph (DAG) of processes with dependency awareness,
retry logic, and a built-in web dashboard.

All commands communicate with the running Husky daemon over a Unix socket.`,
		SilenceUsage: true,
		// Print version when --version flag is provided.
		Version: fmt.Sprintf("%s (commit %s, built %s)",
			version.Version, version.Commit, version.BuildDate),
	}

	root.SetVersionTemplate("husky {{.Version}}\n")

	root.PersistentFlags().StringVarP(&dataDir, "data", "d", ".husky",
		"data directory (socket, PID file, database)")
	root.PersistentFlags().StringVarP(&configPath, "config", "c", "husky.yaml",
		"path to husky.yaml")

	root.AddCommand(
		newVersionCmd(),
		newDaemonCmd(),
		newStartCmd(),
		newStopCmd(),
		newStatusCmd(),
		newLogsCmd(),
		newHistoryCmd(),
		newValidateCmd(),
		newConfigCmd(),
		newExportCmd(),
		newAuditCmd(),
		newPauseCmd(),
		newResumeCmd(),
		newTagsCmd(),
		newIntegrationsCmd(),
		newRunCmd(),
		newDagCmd(),
		newRetryCmd(),
		newCancelCmd(),
		newSkipCmd(),
		newReloadCmd(),
		newDashCmd(),
	)

	return root
}

// sockPath returns the path to the Unix socket for the current data dir.
func sockPath() string { return filepath.Join(dataDir, "husky.sock") }

// pidFilePath returns the path to the PID file for the current data dir.
func pidFilePath() string { return filepath.Join(dataDir, "husky.pid") }

// apiBase reads the bound API address written by the daemon into <data>/api.addr
// and returns it as a bare host:port string. Falls back to the historic default
// so existing setups without an api.addr file keep working.
func apiBase() string {
	data, err := os.ReadFile(filepath.Join(dataDir, "api.addr"))
	if err != nil {
		return "127.0.0.1:8420"
	}
	return strings.TrimSpace(string(data))
}

// dial opens an IPC connection or exits with an error message.
func dial() *ipc.Client {
	c := ipc.NewClient(sockPath())
	resp, err := c.Do(ipc.Request{Type: ipc.ReqPing})
	if err != nil || !resp.OK {
		fmt.Fprintf(os.Stderr, "error: daemon is not running (no response on %s)\n", sockPath())
		fmt.Fprintf(os.Stderr, "Run `husky start` to start the daemon.\n")
		os.Exit(1)
	}
	return c
}

// jobNamesCompletion provides shell tab-completion candidates for commands that
// accept a job name as their first positional argument. It reads job names
// directly from husky.yaml so it works even when the daemon is not running.
func jobNamesCompletion(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		// Only one job name accepted.
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	names := make([]string, 0, len(cfg.Jobs))
	for name := range cfg.Jobs {
		names = append(names, name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// ─── logs ────────────────────────────────────────────────────────────────────

func newLogsCmd() *cobra.Command {
	var includeHealthcheck bool
	var runID int64
	var tail bool
	var tag string

	cmd := &cobra.Command{
		Use:   "logs [job]",
		Short: "Print logs for a job run",
		Args: func(_ *cobra.Command, args []string) error {
			if tag != "" {
				if len(args) != 0 {
					return fmt.Errorf("job argument cannot be used with --tag")
				}
				if !tail {
					return fmt.Errorf("--tag requires --tail")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("job name is required unless --tag is used")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if tag != "" {
				return tailLogsByTag(tag, includeHealthcheck)
			}
			jobName := args[0]
			dbPath := filepath.Join(dataDir, "husky.db")

			st, err := store.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open store %s: %w", dbPath, err)
			}
			defer func() { _ = st.Close() }()

			ctx := context.Background()
			var run *store.Run
			var runErr error
			if runID > 0 {
				run, runErr = st.GetRun(ctx, runID)
				if runErr != nil {
					return fmt.Errorf("query run %d: %w", runID, runErr)
				}
				if run == nil {
					return fmt.Errorf("run %d not found", runID)
				}
				if run.JobName != jobName {
					return fmt.Errorf("run %d belongs to job %q, not %q", runID, run.JobName, jobName)
				}
			} else {
				run, runErr = st.GetLastRunForJob(ctx, jobName)
				if runErr != nil {
					return fmt.Errorf("query last run for %q: %w", jobName, runErr)
				}
			}
			if run == nil {
				return fmt.Errorf("no runs found for job %q", jobName)
			}

			if tail {
				dial() // exits if daemon not running
				return streamRunLogs(run.ID, includeHealthcheck)
			}

			lines, err := st.GetRunLogs(ctx, run.ID)
			if err != nil {
				return fmt.Errorf("query logs for run %d: %w", run.ID, err)
			}

			for _, line := range lines {
				if !includeHealthcheck && line.Stream == "healthcheck" {
					continue
				}
				fmt.Println(line.Line)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&includeHealthcheck, "include-healthcheck", false,
		"include healthcheck log lines in output")
	cmd.Flags().Int64Var(&runID, "run", 0, "show logs for a specific run ID")
	cmd.Flags().BoolVar(&tail, "tail", false, "stream live logs until run completion")
	cmd.Flags().StringVar(&tag, "tag", "", "stream live logs for all jobs with a tag (requires --tail)")
	cmd.ValidArgsFunction = jobNamesCompletion
	return cmd
}

func tailLogsByTag(tag string, includeHealthcheck bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	tagJobs := map[string]bool{}
	for name, job := range cfg.Jobs {
		for _, t := range job.Tags {
			if t == tag {
				tagJobs[name] = true
				break
			}
		}
	}
	if len(tagJobs) == 0 {
		return fmt.Errorf("no jobs found for tag %q", tag)
	}

	dial()
	dbPath := filepath.Join(dataDir, "husky.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	seen := map[int64]bool{}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		runs, err := st.SearchRuns(ctx, store.RunSearchParams{Status: store.StatusRunning, Limit: 500})
		if err != nil {
			return err
		}
		for _, run := range runs {
			if !tagJobs[run.JobName] || seen[run.ID] {
				continue
			}
			seen[run.ID] = true
			go func(runID int64, jobName string) {
				_ = streamRunLogsWithPrefix(runID, includeHealthcheck, "["+jobName+"] ")
			}(run.ID, run.JobName)
		}
		time.Sleep(time.Second)
	}
}

func streamRunLogs(runID int64, includeHealthcheck bool) error {
	return streamRunLogsWithPrefix(runID, includeHealthcheck, "")
}

func streamRunLogsWithPrefix(runID int64, includeHealthcheck bool, prefix string) error {
	u := url.URL{Scheme: "ws", Host: apiBase(), Path: fmt.Sprintf("/ws/logs/%d", runID)}
	q := u.Query()
	if includeHealthcheck {
		q.Set("include_healthcheck", "true")
	}
	u.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("connect log stream: %w", err)
	}
	defer conn.Close()

	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			return fmt.Errorf("read log stream: %w", err)
		}
		msgType, _ := msg["type"].(string)
		switch msgType {
		case "log":
			line, _ := msg["line"].(string)
			fmt.Println(prefix + line)
		case "end":
			return nil
		case "error":
			text, _ := msg["message"].(string)
			if text == "" {
				text = "unknown websocket error"
			}
			return fmt.Errorf("stream error: %s", text)
		}
	}
}

func newHistoryCmd() *cobra.Command {
	var last int
	cmd := &cobra.Command{
		Use:   "history <job>",
		Short: "Show recent run history for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			jobName := args[0]
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			job, ok := cfg.Jobs[jobName]
			if !ok {
				return fmt.Errorf("unknown job %q", jobName)
			}
			st, err := store.Open(filepath.Join(dataDir, "husky.db"))
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			runs, err := st.GetRunsForJob(context.Background(), jobName, last)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "RUN ID\tSTATUS\tDURATION\tTRIGGER\tREASON\tVS SLA")
			for _, run := range runs {
				dur := "-"
				if run.StartedAt != nil && run.FinishedAt != nil {
					dur = run.FinishedAt.Sub(*run.StartedAt).Round(time.Second).String()
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
					run.ID,
					run.Status,
					dur,
					run.Trigger,
					run.Reason,
					vsSLA(job.SLA, &run),
				)
			}
			return w.Flush()
		},
	}
	cmd.Flags().IntVar(&last, "last", 20, "number of runs to show")
	cmd.ValidArgsFunction = jobNamesCompletion
	return cmd
}

func vsSLA(rawSLA string, run *store.Run) string {
	if strings.TrimSpace(rawSLA) == "" || run.StartedAt == nil || run.FinishedAt == nil {
		return "-"
	}
	sla, err := time.ParseDuration(rawSLA)
	if err != nil {
		return "-"
	}
	delta := run.FinishedAt.Sub(*run.StartedAt) - sla
	if delta <= 0 {
		return "✓"
	}
	return "⚠ +" + delta.Round(time.Second).String()
}

func newValidateCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate husky.yaml",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if _, err := dag.Build(cfg); err != nil {
				return err
			}
			if strict {
				for name, job := range cfg.Jobs {
					if strings.TrimSpace(job.Description) == "" {
						fmt.Fprintf(os.Stderr, "warning: job %s has no description\n", name)
					}
					if job.Notify == nil {
						fmt.Fprintf(os.Stderr, "warning: job %s has no notify configuration\n", name)
					}
				}
			}
			fmt.Println("✓ husky.yaml is valid")

			// Also validate huskyd.yaml when present next to husky.yaml.
			huskyDir := filepath.Dir(configPath)
			_, err = daemoncfg.Load("", huskyDir)
			if err != nil {
				return fmt.Errorf("huskyd.yaml: %w", err)
			}
			// Check whether a huskyd.yaml was actually present.
			daemonCfgCandidate := filepath.Join(huskyDir, "huskyd.yaml")
			if _, statErr := os.Stat(daemonCfgCandidate); statErr == nil {
				fmt.Println("✓ huskyd.yaml is valid")
			}

			return nil
		},
		Args: cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "warn on missing optional best-practice fields")
	return cmd
}

func newConfigCmd() *cobra.Command {
	show := &cobra.Command{Use: "show", Short: "Print effective config", RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		raw, err := yaml.Marshal(cfg)
		if err != nil {
			return err
		}
		fmt.Print(string(raw))
		return nil
	}}
	cmd := &cobra.Command{Use: "config", Short: "Configuration utilities"}
	cmd.AddCommand(show)
	return cmd
}

func newExportCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export scheduler state snapshot",
		RunE: func(_ *cobra.Command, _ []string) error {
			if format != "json" {
				return fmt.Errorf("unsupported format %q", format)
			}
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			huskyDir := filepath.Dir(configPath)
			dcfg, _ := daemoncfg.Load("", huskyDir)
			dbPath := dcfg.Storage.SQLite.Path
			if dbPath == "" {
				dbPath = filepath.Join(dataDir, "husky.db")
			}
			st, err := store.OpenWithConfig(dbPath, store.StoreConfig{})
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			states, _ := st.ListJobStates(context.Background())
			runs, _ := st.SearchRuns(context.Background(), store.RunSearchParams{Limit: 1000})
			alerts, _ := st.ListAlerts(context.Background(), 1000)
			raw, _ := json.MarshalIndent(map[string]any{"config": cfg, "job_state": states, "runs": runs, "alerts": alerts}, "", "  ")
			fmt.Println(string(raw))
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "export format")
	return cmd
}

func newAuditCmd() *cobra.Command {
	var job, trigger, status, since, reason, tag, export string
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Search run history across all jobs",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			st, err := store.Open(filepath.Join(dataDir, "husky.db"))
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			var sinceTime *time.Time
			if strings.TrimSpace(since) != "" {
				t, err := time.Parse(time.RFC3339, since)
				if err != nil {
					return fmt.Errorf("--since must be RFC3339")
				}
				sinceTime = &t
			}
			runs, err := st.SearchRuns(context.Background(), store.RunSearchParams{
				Job: job, Trigger: store.Trigger(trigger), Status: store.RunStatus(strings.ToUpper(status)), Since: sinceTime, Reason: reason, Limit: 5000,
			})
			if err != nil {
				return err
			}
			if tag != "" {
				filtered := make([]store.Run, 0, len(runs))
				for _, run := range runs {
					j, ok := cfg.Jobs[run.JobName]
					if !ok {
						continue
					}
					for _, t := range j.Tags {
						if t == tag {
							filtered = append(filtered, run)
							break
						}
					}
				}
				runs = filtered
			}

			if export == "csv" {
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"id", "job", "status", "trigger", "reason", "created_at"})
				for _, run := range runs {
					_ = w.Write([]string{strconv.FormatInt(run.ID, 10), run.JobName, string(run.Status), string(run.Trigger), run.Reason, run.CreatedAt.UTC().Format(time.RFC3339)})
				}
				w.Flush()
				return w.Error()
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "RUN ID\tJOB\tSTATUS\tTRIGGER\tREASON\tCREATED")
			for _, run := range runs {
				ts := run.CreatedAt.UTC().Format("Jan 02 15:04 UTC")
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", run.ID, run.JobName, run.Status, run.Trigger, run.Reason, ts)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&job, "job", "", "filter by job")
	cmd.Flags().StringVar(&trigger, "trigger", "", "filter by trigger: manual|schedule|dependency")
	cmd.Flags().StringVar(&status, "status", "", "filter by status: failed|success|skipped")
	cmd.Flags().StringVar(&since, "since", "", "filter runs since RFC3339 timestamp")
	cmd.Flags().StringVar(&reason, "reason", "", "filter by reason text")
	cmd.Flags().StringVar(&tag, "tag", "", "filter by job tag")
	cmd.Flags().StringVar(&export, "export", "", "export format (csv)")
	return cmd
}

func newPauseCmd() *cobra.Command {
	var tag string
	cmd := &cobra.Command{Use: "pause", Short: "Pause jobs", RunE: func(_ *cobra.Command, _ []string) error {
		if tag == "" {
			return fmt.Errorf("--tag is required")
		}
		resp, err := authPost("http://" + apiBase() + "/api/jobs/pause?tag=" + url.QueryEscape(tag))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("pause request failed: status %d", resp.StatusCode)
		}
		fmt.Printf("paused jobs with tag %q\n", tag)
		return nil
	}}
	cmd.Flags().StringVar(&tag, "tag", "", "pause all jobs with tag")
	return cmd
}

func newResumeCmd() *cobra.Command {
	var tag string
	cmd := &cobra.Command{Use: "resume", Short: "Resume paused jobs", RunE: func(_ *cobra.Command, _ []string) error {
		if tag == "" {
			return fmt.Errorf("--tag is required")
		}
		resp, err := authPost("http://" + apiBase() + "/api/jobs/resume?tag=" + url.QueryEscape(tag))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("resume request failed: status %d", resp.StatusCode)
		}
		fmt.Printf("resumed jobs with tag %q\n", tag)
		return nil
	}}
	cmd.Flags().StringVar(&tag, "tag", "", "resume all jobs with tag")
	return cmd
}

func newTagsCmd() *cobra.Command {
	list := &cobra.Command{Use: "list", Short: "List all configured tags", RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		counts := map[string]int{}
		for _, job := range cfg.Jobs {
			for _, tag := range job.Tags {
				counts[tag]++
			}
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TAG\tCOUNT")
		for tag, count := range counts {
			fmt.Fprintf(w, "%s\t%d\n", tag, count)
		}
		return w.Flush()
	}}
	cmd := &cobra.Command{Use: "tags", Short: "Tag utilities"}
	cmd.AddCommand(list)
	return cmd
}

func newIntegrationsCmd() *cobra.Command {
	list := &cobra.Command{Use: "list", Short: "List configured integrations", RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPROVIDER\tCREDENTIALS")
		for name, intg := range cfg.Integrations {
			provider := intg.EffectiveProvider
			if provider == "" {
				provider = intg.Provider
			}
			if provider == "" {
				provider = name
			}
			status := "set"
			switch provider {
			case "slack", "discord", "webhook":
				if strings.TrimSpace(intg.WebhookURL) == "" { status = "missing" }
			case "pagerduty":
				if strings.TrimSpace(intg.RoutingKey) == "" { status = "missing" }
			case "smtp":
				if strings.TrimSpace(intg.Host) == "" || strings.TrimSpace(intg.From) == "" { status = "missing" }
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", name, provider, status)
		}
		return w.Flush()
	}}
	test := &cobra.Command{Use: "test <name>", Short: "Send integration test event", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		st, err := store.Open(filepath.Join(dataDir, "husky.db"))
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		d := notify.New(st, nil)
		if err := d.TestIntegration(context.Background(), cfg, args[0]); err != nil {
			return err
		}
		fmt.Printf("integration %q test sent\n", args[0])
		return nil
	}}
	cmd := &cobra.Command{Use: "integrations", Short: "Integration utilities"}
	cmd.AddCommand(list, test)
	return cmd
}

// ─── version ──────────────────────────────────────────────────────────────────

// newVersionCmd returns the `husky version` sub-command.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("husky %s\ncommit:     %s\nbuilt:      %s\n",
				version.Version, version.Commit, version.BuildDate)
		},
	}
}

// ─── start ────────────────────────────────────────────────────────────────────

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Husky background daemon",
		Long: `Start the Husky background daemon.

The daemon is launched from the current husky binary and runs in the
background. husky start returns once the daemon socket is ready.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			huskydPath, err := currentExecutable()
			if err != nil {
				return fmt.Errorf("cannot determine husky executable: %w", err)
			}

			args := []string{
				"daemon",
				"run",
				"--config", configPath,
				"--data", dataDir,
			}

			// Background mode.
			if err := os.MkdirAll(dataDir, 0o755); err != nil {
				return fmt.Errorf("cannot create data directory %s: %w", dataDir, err)
			}
			logPath := filepath.Join(dataDir, "huskyd.log")
			logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("cannot open log file %s: %w", logPath, err)
			}

			c := exec.Command(huskydPath, args...)
			c.Stdout = logFile
			c.Stderr = logFile
			c.Stdin = nil
			detachProcess(c)

			if err := c.Start(); err != nil {
				_ = logFile.Close()
				return fmt.Errorf("failed to start daemon: %w", err)
			}
			_ = logFile.Close()

			waitCh := make(chan error, 1)
			go func() {
				waitCh <- c.Wait()
			}()

			// Wait for the socket to appear (up to 5 seconds).
			sock := sockPath()
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(sock); err == nil {
					fmt.Printf("daemon started (pid %d)\n", c.Process.Pid)
					fmt.Printf("log:    %s\n", logPath)
					fmt.Printf("socket: %s\n", sock)
					return nil
				}
				select {
				case err := <-waitCh:
					if err == nil {
						return fmt.Errorf("daemon exited before becoming ready; see log: %s", logPath)
					}
					return fmt.Errorf("daemon exited before becoming ready: %w (log: %s)", err, logPath)
				default:
				}
				time.Sleep(100 * time.Millisecond)
			}

			fmt.Printf("daemon started (pid %d) but socket not ready yet\n", c.Process.Pid)
			fmt.Printf("log:    %s\n", logPath)
			return nil
		},
	}
	return cmd
}

func newDaemonCmd() *cobra.Command {
	var daemonConfigPath string

	daemon := &cobra.Command{
		Use:    "daemon",
		Short:  "Internal daemon commands",
		Hidden: true,
	}

	run := &cobra.Command{
		Use:    "run",
		Short:  "Run the Husky daemon in the foreground",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return daemoncmd.Run(daemoncmd.Options{
				ConfigPath:       configPath,
				DataDir:          dataDir,
				DaemonConfigPath: daemonConfigPath,
			})
		},
	}
	run.Flags().StringVar(&daemonConfigPath, "daemon-config", "",
		"path to huskyd.yaml (default: <config dir>/huskyd.yaml)")

	daemon.AddCommand(run)
	return daemon
}

func currentExecutable() (string, error) {
	if exe, err := os.Executable(); err == nil {
		return exe, nil
	}
	return exec.LookPath("husky")
}

// ─── stop ─────────────────────────────────────────────────────────────────────

// spinFrames is the braille spinner sequence used while draining in-flight jobs.
var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func newStopCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running Husky daemon",
		RunE: func(_ *cobra.Command, _ []string) error {
			if force {
				return forceStop()
			}
			client := ipc.NewClient(sockPath())

			// Snapshot running jobs before we initiate the drain so we can
			// show the user what's in flight. Failure here is non-fatal.
			var inFlight []string
			if sr, err := client.Do(ipc.Request{Type: ipc.ReqStatus}); err == nil && sr.OK {
				var statuses []ipc.JobStatus
				if json.Unmarshal(sr.Data, &statuses) == nil {
					for _, s := range statuses {
						if s.Running {
							inFlight = append(inFlight, s.Name)
						}
					}
				}
			}

			// Send the graceful stop signal.
			resp, err := client.Do(ipc.Request{Type: ipc.ReqStop})
			if err != nil {
				return fmt.Errorf("failed to contact daemon: %w", err)
			}
			if !resp.OK {
				return fmt.Errorf("daemon returned error: %s", resp.Error)
			}

			if len(inFlight) == 0 {
				// No jobs were running — daemon exits almost immediately.
				fmt.Println("✓ daemon shutdown successfully")
				return nil
			}

			// Jobs are draining — keep the terminal alive with a spinner.
			fmt.Printf("Draining %d in-flight job(s): %s\n",
				len(inFlight), strings.Join(inFlight, ", "))
			return waitForShutdown(client)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false,
		"forcibly kill the daemon via its PID file (SIGKILL)")
	return cmd
}

// waitForShutdown polls the daemon's status every 250 ms and renders an
// interactive spinner showing which jobs are still draining. It returns once
// the daemon's Unix socket stops accepting connections (daemon has exited).
func waitForShutdown(client *ipc.Client) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	frame := 0
	for range ticker.C {
		sr, err := client.Do(ipc.Request{Type: ipc.ReqStatus})
		if err != nil || !sr.OK {
			// Socket gone — daemon has exited.
			fmt.Print("\r\033[K") // clear spinner line
			fmt.Println("✓ daemon shutdown successfully")
			return nil
		}

		// Collect still-running jobs for the display line.
		var statuses []ipc.JobStatus
		var running []string
		if json.Unmarshal(sr.Data, &statuses) == nil {
			for _, s := range statuses {
				if s.Running {
					running = append(running, s.Name)
				}
			}
		}

		spin := spinFrames[frame%len(spinFrames)]
		frame++

		if len(running) == 0 {
			fmt.Printf("\r\033[K%s waiting for daemon to exit...", spin)
		} else {
			fmt.Printf("\r\033[K%s draining (%d): %s",
				spin, len(running), strings.Join(running, ", "))
		}
	}
	return nil
}

// forceStop reads the PID file and sends SIGKILL to the daemon.
func forceStop() error {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return fmt.Errorf("cannot read PID file %s: %w", pidFilePath(), err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("cannot find process %d: %w", pid, err)
	}
	if err := proc.Kill(); err != nil {
		return fmt.Errorf("kill %d: %w", pid, err)
	}
	fmt.Printf("sent SIGKILL to daemon (pid %d)\n", pid)
	return nil
}

// ─── status ───────────────────────────────────────────────────────────────────

func newStatusCmd() *cobra.Command {
	var tag string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the status of all configured jobs",
		RunE: func(_ *cobra.Command, _ []string) error {
			dial() // exits if daemon not running
			client := ipc.NewClient(sockPath())
			resp, err := client.Do(ipc.Request{Type: ipc.ReqStatus})
			if err != nil {
				return fmt.Errorf("status request failed: %w", err)
			}
			if !resp.OK {
				return fmt.Errorf("daemon error: %s", resp.Error)
			}
			var statuses []ipc.JobStatus
			if err := json.Unmarshal(resp.Data, &statuses); err != nil {
				return fmt.Errorf("malformed status response: %w", err)
			}
			if strings.TrimSpace(tag) != "" {
				tagged, err := jobsForTag(tag)
				if err != nil {
					return err
				}
				filtered := make([]ipc.JobStatus, 0, len(statuses))
				for _, status := range statuses {
					if tagged[status.Name] {
						filtered = append(filtered, status)
					}
				}
				statuses = filtered
			}
			printStatusTable(statuses)
			return nil
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "show status for jobs with tag")
	return cmd
}

func printStatusTable(statuses []ipc.JobStatus) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tNEXT RUN\tLAST SUCCESS\tLAST FAILURE")
	for _, s := range statuses {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			s.Name,
			fmtTime(s.NextRun),
			fmtTime(s.LastSuccess),
			fmtTime(s.LastFailure),
		)
	}
	_ = w.Flush()
}

func orDash(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
}

// fmtTime parses an RFC3339 timestamp string and returns it in a compact,
// human-readable form: "Jan 02 15:04 UTC".
// Returns "-" if the pointer is nil or the string is empty/unparseable.
func fmtTime(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return *s // fallback to raw string
	}
	return t.UTC().Format("Jan 02 15:04 UTC")
}

// ─── run ──────────────────────────────────────────────────────────────────────

func newRunCmd() *cobra.Command {
	var reason string
	var tag string

	cmd := &cobra.Command{
		Use:   "run [job]",
		Short: "Trigger an immediate manual run of a job",
		Args: func(_ *cobra.Command, args []string) error {
			if strings.TrimSpace(tag) != "" {
				if len(args) > 0 {
					return fmt.Errorf("job argument cannot be combined with --tag")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("job name is required unless --tag is used")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			dial() // exits if daemon not running
			client := ipc.NewClient(sockPath())
			if len(reason) > 500 {
				return fmt.Errorf("--reason exceeds max length of 500 characters")
			}

			if strings.TrimSpace(tag) != "" {
				tagged, err := jobsForTag(tag)
				if err != nil {
					return err
				}
				if len(tagged) == 0 {
					return fmt.Errorf("no jobs found for tag %q", tag)
				}
				triggered := 0
				for jobName := range tagged {
					resp, err := client.Do(ipc.Request{Type: ipc.ReqRun, Job: jobName, Reason: reason})
					if err != nil {
						return fmt.Errorf("run request failed for %q: %w", jobName, err)
					}
					if !resp.OK {
						return fmt.Errorf("daemon error for %q: %s", jobName, resp.Error)
					}
					triggered++
				}
				fmt.Printf("triggered %d job(s) with tag %q\n", triggered, tag)
				return nil
			}

			resp, err := client.Do(ipc.Request{Type: ipc.ReqRun, Job: args[0], Reason: reason})
			if err != nil {
				return fmt.Errorf("run request failed: %w", err)
			}
			if !resp.OK {
				return fmt.Errorf("daemon error: %s", resp.Error)
			}
			fmt.Printf("job %q triggered\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVarP(&reason, "reason", "r", "", "optional reason annotation")
	cmd.Flags().StringVar(&tag, "tag", "", "trigger all jobs with tag")
	cmd.ValidArgsFunction = jobNamesCompletion
	return cmd
}

func jobsForTag(tag string) (map[string]bool, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	jobs := map[string]bool{}
	for name, job := range cfg.Jobs {
		for _, t := range job.Tags {
			if t == tag {
				jobs[name] = true
				break
			}
		}
	}
	return jobs, nil
}

// ─── dag ──────────────────────────────────────────────────────────────────────

func newDagCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "dag",
		Short: "Show the job dependency graph",
		Long: `Print the dependency graph for all jobs.

By default the output is a human-readable ASCII listing. Use --json to get a
machine-readable JSON array suitable for piping to jq or other tools.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			dial()
			client := ipc.NewClient(sockPath())
			resp, err := client.Do(ipc.Request{Type: ipc.ReqDag, JSON: asJSON})
			if err != nil {
				return fmt.Errorf("dag request failed: %w", err)
			}
			if !resp.OK {
				return fmt.Errorf("daemon error: %s", resp.Error)
			}
			if asJSON {
				// Pretty-print the JSON.
				var v interface{}
				if err := json.Unmarshal(resp.Data, &v); err != nil {
					fmt.Printf("%s\n", resp.Data)
					return nil
				}
				out, _ := json.MarshalIndent(v, "", "  ")
				fmt.Println(string(out))
			} else {
				// Data is a JSON-encoded string; decode it back to plain text.
				var text string
				if err := json.Unmarshal(resp.Data, &text); err != nil {
					fmt.Print(string(resp.Data))
					return nil
				}
				fmt.Print(text)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

// ─── dash ─────────────────────────────────────────────────────────────────────

func newDashCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dash",
		Short: "Open the Husky dashboard in your browser",
		Long: `Read the daemon's API address from <data>/api.addr and open the
dashboard in the default browser. The daemon must be running.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			addrBytes, err := os.ReadFile(filepath.Join(dataDir, "api.addr"))
			if err != nil {
				return fmt.Errorf("daemon is not running (no api.addr in %s — run `husky start`)", dataDir)
			}
			dashURL := "http://" + strings.TrimSpace(string(addrBytes))
			fmt.Printf("Opening dashboard: %s\n", dashURL)
			return openBrowser(dashURL)
		},
	}
}

// openBrowser opens url in the default system browser.
func openBrowser(rawURL string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", rawURL}
	case "darwin":
		cmd = "open"
		args = []string{rawURL}
	default: // linux / bsd
		cmd = "xdg-open"
		args = []string{rawURL}
	}
	return exec.Command(cmd, args...).Start()
}

// ─── retry ────────────────────────────────────────────────────────────────────

func newRetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retry <job>",
		Short: "Immediately retry a failed job",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			dial()
			client := ipc.NewClient(sockPath())
			resp, err := client.Do(ipc.Request{
				Type:   ipc.ReqRetry,
				Job:    args[0],
				Reason: "manual retry",
			})
			if err != nil {
				return fmt.Errorf("retry request failed: %w", err)
			}
			if !resp.OK {
				return fmt.Errorf("daemon error: %s", resp.Error)
			}
			fmt.Printf("retry triggered for job %q\n", args[0])
			return nil
		},
	}
	cmd.ValidArgsFunction = jobNamesCompletion
	return cmd
}

// ─── cancel ───────────────────────────────────────────────────────────────────

func newCancelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel <job>",
		Short: "Cancel the currently running instance of a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			dial()
			client := ipc.NewClient(sockPath())
			resp, err := client.Do(ipc.Request{
				Type: ipc.ReqCancel,
				Job:  args[0],
			})
			if err != nil {
				return fmt.Errorf("cancel request failed: %w", err)
			}
			if !resp.OK {
				return fmt.Errorf("daemon error: %s", resp.Error)
			}
			fmt.Printf("job %q cancelled\n", args[0])
			return nil
		},
	}
	cmd.ValidArgsFunction = jobNamesCompletion
	return cmd
}

// ─── skip ─────────────────────────────────────────────────────────────────────

func newSkipCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skip <job>",
		Short: "Skip the next pending run of a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			dial()
			client := ipc.NewClient(sockPath())
			resp, err := client.Do(ipc.Request{
				Type: ipc.ReqSkip,
				Job:  args[0],
			})
			if err != nil {
				return fmt.Errorf("skip request failed: %w", err)
			}
			if !resp.OK {
				return fmt.Errorf("daemon error: %s", resp.Error)
			}
			fmt.Printf("job %q skipped\n", args[0])
			return nil
		},
	}
	cmd.ValidArgsFunction = jobNamesCompletion
	return cmd
}

// ─── reload ───────────────────────────────────────────────────────────────────

func newReloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Hot-reload the daemon configuration without restarting",
		RunE: func(_ *cobra.Command, _ []string) error {
			dial()
			client := ipc.NewClient(sockPath())
			resp, err := client.Do(ipc.Request{Type: ipc.ReqReload})
			if err != nil {
				return fmt.Errorf("reload request failed: %w", err)
			}
			if !resp.OK {
				return fmt.Errorf("daemon error: %s", resp.Error)
			}
			fmt.Println("config reloaded")
			return nil
		},
	}
}

// authPost sends an authenticated HTTP POST to the given URL.
// If the HUSKY_TOKEN environment variable is set, its value is injected as
// an Authorization: Bearer header so the request is accepted by a huskyd
// instance running with auth enabled.
func authPost(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if tok := os.Getenv("HUSKY_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return http.DefaultClient.Do(req)
}

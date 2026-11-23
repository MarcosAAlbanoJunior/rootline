package recording

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

// Default ports of the recording environment.
const (
	DefaultPostgresPort  = 55432
	DefaultCollectorPort = 54318
	DefaultAppPort       = 58080
)

// RecordOptions configures Record.
type RecordOptions struct {
	ScenarioDir, SourceDir, OutDir       string
	Load                                 LoadConfig
	PostgresPort, CollectorPort, AppPort int // 0 = default
}

// Record generates <OutDir>/repo, builds and runs every commit under the fixed load, and writes
// <OutDir>/traces.jsonl and <OutDir>/manifest.json. It always records from scratch and fails if
// OutDir exists and is not empty.
func Record(ctx context.Context, opts RecordOptions) (err error) {
	if opts.PostgresPort == 0 {
		opts.PostgresPort = DefaultPostgresPort
	}
	if opts.CollectorPort == 0 {
		opts.CollectorPort = DefaultCollectorPort
	}
	if opts.AppPort == 0 {
		opts.AppPort = DefaultAppPort
	}
	if opts.Load.Requests == 0 && opts.Load.Rate == 0 && len(opts.Load.Routes) == 0 {
		opts.Load = DefaultLoad()
	}
	if _, err := opts.Load.cycle(); err != nil {
		return err // before generating or starting anything
	}
	if _, lookErr := exec.LookPath("docker"); lookErr != nil {
		return errors.New("recording: docker is required to record a scenario")
	}

	gen, err := fixture.Generate(fixture.Options{ScenarioDir: opts.ScenarioDir, SourceDir: opts.SourceDir, OutDir: opts.OutDir})
	if err != nil {
		return err
	}
	sc, err := fixture.LoadScenario(opts.ScenarioDir)
	if err != nil {
		return err
	}

	work, err := os.MkdirTemp("", "rootline-record-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }() // best-effort cleanup of a temp dir

	binaries := map[string]string{}
	for _, c := range gen.Commits {
		bin, err := buildVersion(ctx, gen.RepoDir, work, c)
		if err != nil {
			return err
		}
		binaries[c.SHA] = bin
	}

	stack, err := startStack(ctx, opts, work)
	if err != nil {
		return err
	}
	defer func() {
		if downErr := stack.down(); err == nil {
			err = downErr
		}
	}()

	for _, c := range gen.Commits {
		if err := runVersion(ctx, opts, binaries[c.SHA], c.SHA); err != nil {
			return fmt.Errorf("recording: version %s (%s): %w", c.Label, c.SHA, err)
		}
	}
	if err := stack.flushCollector(ctx); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(stack.collectorOut, "traces.jsonl"), filepath.Join(opts.OutDir, "traces.jsonl")); err != nil {
		return fmt.Errorf("recording: collector wrote no traces file: %w", err)
	}

	return WriteManifest(filepath.Join(opts.OutDir, "manifest.json"), buildManifest(sc, gen, opts.Load))
}

// buildManifest describes a recording: scenario, symptom, commits, answer key, seed and load.
func buildManifest(sc fixture.Scenario, gen fixture.Generated, load LoadConfig) Manifest {
	m := Manifest{Scenario: sc.Name, AnswerKey: gen.AnswerKey, Seed: SeedID, Load: load, Symptom: sc.Symptom}
	for _, c := range gen.Commits {
		m.Commits = append(m.Commits, CommitRef{Label: c.Label, SHA: c.SHA})
	}
	return m
}

// buildVersion exports the commit with git archive and builds its demo-app.
func buildVersion(ctx context.Context, repo, work string, c fixture.Commit) (string, error) {
	src := filepath.Join(work, "src-"+c.Label)
	if err := os.MkdirAll(src, 0o755); err != nil {
		return "", err
	}
	archive, err := runCmd(ctx, repo, nil, "git", "archive", "--format=tar", c.SHA)
	if err != nil {
		return "", err
	}
	if err := untar(bytes.NewReader([]byte(archive)), src); err != nil {
		return "", err
	}
	bin := filepath.Join(work, "demo-app-"+c.Label)
	env := []string{"GOWORK=off", "CGO_ENABLED=0"}
	if _, err := runCmd(ctx, filepath.Join(src, "demo-app"), env, "go", "build", "-o", bin, "."); err != nil {
		return "", fmt.Errorf("recording: building demo-app at %s (needs network or a warm module cache): %w", c.SHA, err)
	}
	return bin, nil
}

func untar(r io.Reader, dst string) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(h.Name))
		if rel, err := filepath.Rel(dst, target); err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("recording: archive entry %q escapes the destination", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				return err
			}
			if err := os.WriteFile(target, data, fs0644(h.Mode)); err != nil {
				return err
			}
		}
	}
}

func fs0644(mode int64) os.FileMode {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

type stack struct {
	project      string
	composeDir   string
	collectorOut string
	env          []string
}

func startStack(ctx context.Context, opts RecordOptions, work string) (*stack, error) {
	composeDir := filepath.Join(work, "eval")
	if err := os.MkdirAll(composeDir, 0o755); err != nil {
		return nil, err
	}
	for _, f := range []string{"compose.yml", "otelcol.yaml"} {
		if err := copyFile(filepath.Join(opts.SourceDir, "eval", f), filepath.Join(composeDir, f)); err != nil {
			return nil, err
		}
	}
	out := filepath.Join(work, "collector-out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	if err := os.Chmod(out, 0o777); err != nil { // the Collector runs as UID 10001
		return nil, err
	}
	s := &stack{
		project:      "rootline-record-" + strconv.FormatInt(time.Now().UnixNano(), 36),
		composeDir:   composeDir,
		collectorOut: out,
		env: []string{
			"ROOTLINE_PG_PORT=" + strconv.Itoa(opts.PostgresPort),
			"ROOTLINE_OTLP_PORT=" + strconv.Itoa(opts.CollectorPort),
			"ROOTLINE_COLLECTOR_OUT=" + out,
		},
	}
	if _, err := s.compose(ctx, "up", "-d"); err != nil {
		_ = s.down()
		return nil, err
	}
	if err := s.waitPostgres(ctx); err != nil {
		_ = s.down()
		return nil, err
	}
	return s, nil
}

func (s *stack) compose(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"compose", "-p", s.project, "-f", "compose.yml"}, args...)
	return runCmd(ctx, s.composeDir, s.env, "docker", full...)
}

// waitPostgres waits for the second "ready to accept connections" line: the image starts a
// temporary server for initialization before the real one.
func (s *stack) waitPostgres(ctx context.Context) error {
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		logs, err := s.compose(ctx, "logs", "postgres")
		if err == nil && strings.Count(logs, "ready to accept connections") >= 2 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return errors.New("recording: postgres did not become ready in 120s")
}

// flushCollector stops the Collector so its file exporter flushes the traces file.
func (s *stack) flushCollector(ctx context.Context) error {
	_, err := s.compose(ctx, "stop", "otelcol")
	return err
}

func (s *stack) down() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := s.compose(ctx, "down", "-v", "--remove-orphans")
	return err
}

// runVersion starts one demo-app, waits for /healthz, runs the load and stops it with SIGINT.
func runVersion(ctx context.Context, opts RecordOptions, bin, sha string) error {
	base := "http://127.0.0.1:" + strconv.Itoa(opts.AppPort)
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"DATABASE_URL=postgres://postgres:rootline@127.0.0.1:"+strconv.Itoa(opts.PostgresPort)+"/shop",
		"SERVICE_VERSION="+sha,
		"LISTEN_ADDR=127.0.0.1:"+strconv.Itoa(opts.AppPort),
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:"+strconv.Itoa(opts.CollectorPort),
	)
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	stop := func() error {
		_ = cmd.Process.Signal(syscall.SIGINT)
		select {
		case err := <-exited:
			return err
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited //nolint:errcheck // the process was killed; its exit error says nothing new
			return errors.New("demo-app did not stop after SIGINT")
		}
	}

	if err := waitHealthy(ctx, healthClient, base+"/healthz", exited, healthTimeout); err != nil {
		_ = stop()
		return fmt.Errorf("%w\n%s", err, logs.String())
	}
	loadErr := RunLoad(ctx, base, opts.Load)
	stopErr := stop()
	if loadErr != nil {
		return fmt.Errorf("%w\n%s", loadErr, logs.String())
	}
	return stopErr
}

// Health check bounds: each probe and the whole wait are time-limited.
var healthClient = &http.Client{Timeout: 2 * time.Second}

const healthTimeout = 60 * time.Second

func waitHealthy(ctx context.Context, client *http.Client, url string, exited <-chan error, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			return fmt.Errorf("demo-app exited early: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
	return fmt.Errorf("demo-app did not become healthy in %s", timeout)
}

func runCmd(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

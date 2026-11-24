// Command demo-app is the Rootline evaluation target: a small orders service.
//
// Env: DATABASE_URL, SERVICE_VERSION, LISTEN_ADDR and the standard OTEL_EXPORTER_OTLP_ENDPOINT.
package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/multitracer"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/MarcosAAlbanoJunior/rootline/demo-app/internal/orders"
	callerhook "github.com/MarcosAAlbanoJunior/rootline/hook"
)

// schemaFS holds the SQL scripts applied at startup. A scenario adds its own seed as another file
// of the folder, applied after the base by name.
//
//go:embed schema/*.sql
var schemaFS embed.FS

func main() {
	if err := run(); err != nil {
		slog.Error("demo-app failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithInsecure()) // endpoint from OTEL_EXPORTER_OTLP_ENDPOINT
	if err != nil {
		return err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", "demo-app"),
			attribute.String("service.version", envOr("SERVICE_VERSION", "dev")),
		)),
	)
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	cfg.ConnConfig.Tracer = multitracer.New(otelpgx.NewTracer(), callerhook.New()) // order matters: hook after otelpgx
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := applySchema(ctx, pool); err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	orders.NewHandler(orders.NewService(orders.NewRepository(pool))).Register(mux)

	srv := &http.Server{
		Addr:              envOr("LISTEN_ADDR", "127.0.0.1:8080"),
		Handler:           otelhttp.NewHandler(mux, "server"),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	slog.Info("listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func applySchema(ctx context.Context, pool *pgxpool.Pool) error {
	scripts, err := schemaScripts(schemaFS)
	if err != nil {
		return err
	}
	for _, sql := range scripts {
		// Simple protocol so each multi-statement script runs in one round trip.
		if _, err := pool.Exec(ctx, sql, pgx.QueryExecModeSimpleProtocol); err != nil {
			return err
		}
	}
	return nil
}

// schemaScripts returns the contents of the schema/*.sql files in lexicographic order of name.
func schemaScripts(fsys fs.FS) ([]string, error) {
	names, err := fs.Glob(fsys, "schema/*.sql")
	if err != nil {
		return nil, err
	}
	// fs.Glob already sorts, but the order is the contract, so it is not left implicit.
	sort.Strings(names)
	scripts := make([]string, 0, len(names))
	for _, n := range names {
		data, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, err
		}
		scripts = append(scripts, string(data))
	}
	return scripts, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

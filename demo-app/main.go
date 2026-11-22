// Command demo-app is the Rootline evaluation target: a small orders service.
//
// Env: DATABASE_URL, SERVICE_VERSION, LISTEN_ADDR and the standard OTEL_EXPORTER_OTLP_ENDPOINT.
package main

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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

//go:embed schema.sql
var schemaSQL string

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
	// Simple protocol so the multi-statement script runs in one round trip.
	_, err := pool.Exec(ctx, schemaSQL, pgx.QueryExecModeSimpleProtocol)
	return err
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

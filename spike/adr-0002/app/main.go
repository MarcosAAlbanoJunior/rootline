// Command app is the minimal demo service of the ADR-0002 spike.
//
// Env: ROOTLINE_HOOK=off disables the caller hook; ROOTLINE_HOOK_CACHE=on enables
// the per-PC cache; SERVICE_VERSION sets service.version.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"

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

	"github.com/MarcosAAlbanoJunior/rootline/spike/adr-0002/app/repo"
	"github.com/MarcosAAlbanoJunior/rootline/spike/adr-0002/app/service"
	"github.com/MarcosAAlbanoJunior/rootline/spike/adr-0002/callerhook"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint("localhost:54318"), otlptracehttp.WithInsecure())
	must(err)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", "spike-shop"),
			attribute.String("service.version", envOr("SERVICE_VERSION", "dev")),
		)),
	)
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	cfg, err := pgxpool.ParseConfig("postgres://postgres:spike@localhost:55432/shop")
	must(err)
	cfg.MaxConns = 8
	tracers := []pgx.QueryTracer{otelpgx.NewTracer()}
	if os.Getenv("ROOTLINE_HOOK") != "off" {
		tracers = append(tracers, callerhook.New(os.Getenv("ROOTLINE_HOOK_CACHE") == "on"))
	}
	cfg.ConnConfig.Tracer = multitracer.New(tracers...)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	must(err)
	defer pool.Close()

	r := &repo.Repo{Pool: pool}
	svc := &service.Service{Repo: r}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /customers/{id}/orders/explicit", func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.ParseInt(req.PathValue("id"), 10, 64)
		orders, err := r.OrdersByCustomer(req.Context(), id)
		if err != nil {
			fail(w, err)
			return
		}
		out := make([]service.OrderWithItems, 0, len(orders))
		for _, o := range orders {
			rows, err := pool.Query(req.Context(), `SELECT sku, qty FROM order_items WHERE order_id = $1 ORDER BY id`, o.ID) // N+1: loop in handler
			if err != nil {
				fail(w, err)
				return
			}
			items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[repo.Item])
			if err != nil {
				fail(w, err)
				return
			}
			out = append(out, service.OrderWithItems{Order: o, Items: items})
		}
		reply(w, out)
	})
	mux.HandleFunc("GET /customers/{id}/orders/helper", serve(svc.Helper))
	mux.HandleFunc("GET /customers/{id}/orders/errgroup", serve(svc.Errgroup))
	mux.HandleFunc("GET /customers/{id}/orders/rangefunc", serve(svc.RangeFunc))

	srv := &http.Server{Addr: "localhost:58080", Handler: otelhttp.NewHandler(mux, "server")}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	slog.Info("listening", "addr", srv.Addr, "hook", os.Getenv("ROOTLINE_HOOK") != "off")
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		must(err)
	}
}

func serve(fn func(context.Context, int64) ([]service.OrderWithItems, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.ParseInt(req.PathValue("id"), 10, 64)
		out, err := fn(req.Context(), id)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, out)
	}
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	slog.Error("request failed", "err", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func must(err error) {
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

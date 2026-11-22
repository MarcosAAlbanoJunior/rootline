// Package orders is the demo-app's orders feature: repository, service and HTTP handler.
package orders

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Querier is the subset of pgxpool.Pool used by the repository.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Order is one row of the orders table.
type Order struct {
	ID         int64     `json:"id"`
	CustomerID int64     `json:"customer_id"`
	PlacedAt   time.Time `json:"placed_at"`
}

// Item is one row of the order_items table.
type Item struct {
	ID      int64  `json:"id"`
	OrderID int64  `json:"order_id"`
	SKU     string `json:"sku"`
	Qty     int    `json:"qty"`
}

// Repository reads orders and items from Postgres.
type Repository struct {
	db Querier
}

// NewRepository returns a Repository over db.
func NewRepository(db Querier) *Repository {
	return &Repository{db: db}
}

// OrdersByCustomer returns the customer's orders, oldest first.
func (r *Repository) OrdersByCustomer(ctx context.Context, customerID int64) ([]Order, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, customer_id, placed_at FROM orders WHERE customer_id = $1 ORDER BY id`, customerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Order])
}

// ItemsByOrders returns the items of all given orders in a single query.
func (r *Repository) ItemsByOrders(ctx context.Context, orderIDs []int64) ([]Item, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, order_id, sku, qty FROM order_items WHERE order_id = ANY($1) ORDER BY id`, orderIDs)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Item])
}

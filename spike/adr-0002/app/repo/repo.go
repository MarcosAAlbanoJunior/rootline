// Package repo is the data access layer of the spike app.
package repo

import (
	"context"
	"iter"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Order struct {
	ID         int64
	TotalCents int64
}

type Item struct {
	SKU string
	Qty int
}

type Repo struct{ Pool *pgxpool.Pool }

func (r *Repo) OrdersByCustomer(ctx context.Context, customerID int64) ([]Order, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id, total_cents FROM orders WHERE customer_id = $1 ORDER BY id`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.TotalCents); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Orders is a range-over-func iterator over a customer's orders.
func (r *Repo) Orders(ctx context.Context, customerID int64) iter.Seq2[Order, error] {
	return func(yield func(Order, error) bool) {
		orders, err := r.OrdersByCustomer(ctx, customerID)
		if err != nil {
			yield(Order{}, err)
			return
		}
		for _, o := range orders {
			if !yield(o, nil) {
				return
			}
		}
	}
}

// ItemsByOrder delegates to a package-level helper, so the query is two frames
// away from the service loop.
func (r *Repo) ItemsByOrder(ctx context.Context, orderID int64) ([]Item, error) {
	return queryItems(ctx, r.Pool, orderID)
}

func queryItems(ctx context.Context, pool *pgxpool.Pool, orderID int64) ([]Item, error) {
	rows, err := pool.Query(ctx, `SELECT sku, qty FROM order_items WHERE order_id = $1 ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.SKU, &it.Qty); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

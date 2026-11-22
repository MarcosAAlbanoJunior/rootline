// Package service holds the N+1 variations that live in a layer above the repository.
package service

import (
	"context"
	"sync"

	"github.com/MarcosAAlbanoJunior/rootline/spike/adr-0002/app/repo"
	"golang.org/x/sync/errgroup"
)

type OrderWithItems struct {
	Order repo.Order
	Items []repo.Item
}

type Service struct{ Repo *repo.Repo }

// Helper: the loop is here, the query is in repo (behind a helper).
func (s *Service) Helper(ctx context.Context, customerID int64) ([]OrderWithItems, error) {
	orders, err := s.Repo.OrdersByCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	out := make([]OrderWithItems, 0, len(orders))
	for _, o := range orders {
		items, err := s.Repo.ItemsByOrder(ctx, o.ID) // N+1: loop line
		if err != nil {
			return nil, err
		}
		out = append(out, OrderWithItems{o, items})
	}
	return out, nil
}

// Errgroup: one goroutine per order.
func (s *Service) Errgroup(ctx context.Context, customerID int64) ([]OrderWithItems, error) {
	orders, err := s.Repo.OrdersByCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	out := make([]OrderWithItems, len(orders))
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for i, o := range orders {
		g.Go(func() error {
			items, err := s.Repo.ItemsByOrder(gctx, o.ID) // N+1: closure inside loop
			if err != nil {
				return err
			}
			mu.Lock()
			out[i] = OrderWithItems{o, items}
			mu.Unlock()
			return nil
		})
	}
	return out, g.Wait()
}

// RangeFunc: iterator over orders with range-over-func.
func (s *Service) RangeFunc(ctx context.Context, customerID int64) ([]OrderWithItems, error) {
	var out []OrderWithItems
	for o, err := range s.Repo.Orders(ctx, customerID) {
		if err != nil {
			return nil, err
		}
		items, err := s.Repo.ItemsByOrder(ctx, o.ID) // N+1: range-over-func body
		if err != nil {
			return nil, err
		}
		out = append(out, OrderWithItems{o, items})
	}
	return out, nil
}

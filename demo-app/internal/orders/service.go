package orders

import "context"

// OrderWithItems is an order and its items.
type OrderWithItems struct {
	Order
	Items []Item `json:"items"`
}

// Store is what the service needs from the repository.
type Store interface {
	OrdersByCustomer(ctx context.Context, customerID int64) ([]Order, error)
	ItemsByOrders(ctx context.Context, orderIDs []int64) ([]Item, error)
}

// Service composes orders with their items.
type Service struct {
	store Store
}

// NewService returns a Service over store.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// OrdersWithItems returns the customer's orders, each with its items.
func (s *Service) OrdersWithItems(ctx context.Context, customerID int64) ([]OrderWithItems, error) {
	list, err := s.store.OrdersByCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(list))
	for _, o := range list {
		ids = append(ids, o.ID)
	}
	items, err := s.store.ItemsByOrders(ctx, ids)
	if err != nil {
		return nil, err
	}
	byOrder := make(map[int64][]Item, len(list))
	for _, it := range items {
		byOrder[it.OrderID] = append(byOrder[it.OrderID], it)
	}
	out := make([]OrderWithItems, 0, len(list))
	for _, o := range list {
		out = append(out, OrderWithItems{Order: o, Items: byOrder[o.ID]})
	}
	return out, nil
}

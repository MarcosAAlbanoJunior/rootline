package orders

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeStore struct {
	orders   []Order
	items    []Item
	ordersEr error
	itemsEr  error

	itemsCalls int
	itemsIDs   []int64
}

func (f *fakeStore) OrdersByCustomer(context.Context, int64) ([]Order, error) {
	return f.orders, f.ordersEr
}

func (f *fakeStore) ItemsByOrders(_ context.Context, ids []int64) ([]Item, error) {
	f.itemsCalls++
	f.itemsIDs = append([]int64(nil), ids...)
	return f.items, f.itemsEr
}

func TestOrdersWithItemsGroupsItemsByTheRightOrder(t *testing.T) {
	st := &fakeStore{
		orders: []Order{{ID: 1}, {ID: 2}},
		items:  []Item{{ID: 10, OrderID: 2}, {ID: 11, OrderID: 1}, {ID: 12, OrderID: 2}},
	}
	got, err := NewService(st).OrdersWithItems(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d orders", len(got))
	}
	ids := func(items []Item) []int64 {
		var out []int64
		for _, it := range items {
			out = append(out, it.ID)
		}
		return out
	}
	if !reflect.DeepEqual(ids(got[0].Items), []int64{11}) || !reflect.DeepEqual(ids(got[1].Items), []int64{10, 12}) {
		t.Fatalf("wrong grouping: %+v", got)
	}
}

func TestOrdersWithItemsOrderWithoutItemsHasNone(t *testing.T) {
	st := &fakeStore{orders: []Order{{ID: 1}, {ID: 2}}, items: []Item{{ID: 10, OrderID: 1}}}
	got, err := NewService(st).OrdersWithItems(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[1].Items) != 0 {
		t.Fatalf("order 2 must have no items, got %+v", got[1].Items)
	}
}

func TestOrdersWithItemsPreservesOrderOfOrders(t *testing.T) {
	st := &fakeStore{orders: []Order{{ID: 5}, {ID: 2}, {ID: 9}}}
	got, err := NewService(st).OrdersWithItems(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, o := range got {
		ids = append(ids, o.ID)
	}
	if !reflect.DeepEqual(ids, []int64{5, 2, 9}) {
		t.Fatalf("order of orders changed: %v", ids)
	}
}

func TestOrdersWithItemsPropagatesStoreErrors(t *testing.T) {
	boom := errors.New("boom")
	t.Run("OrdersByCustomer", func(t *testing.T) {
		st := &fakeStore{ordersEr: boom}
		if _, err := NewService(st).OrdersWithItems(context.Background(), 7); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
	})
	t.Run("ItemsByOrders", func(t *testing.T) {
		st := &fakeStore{orders: []Order{{ID: 1}}, itemsEr: boom}
		if _, err := NewService(st).OrdersWithItems(context.Background(), 7); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
	})
}

func TestOrdersWithItemsCallsItemsByOrdersOnceWithAllIDs(t *testing.T) {
	st := &fakeStore{orders: []Order{{ID: 3}, {ID: 1}, {ID: 2}}}
	if _, err := NewService(st).OrdersWithItems(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if st.itemsCalls != 1 {
		t.Fatalf("ItemsByOrders called %d times, want exactly 1 (no N+1)", st.itemsCalls)
	}
	if !reflect.DeepEqual(st.itemsIDs, []int64{3, 1, 2}) {
		t.Fatalf("ItemsByOrders ids = %v, want [3 1 2]", st.itemsIDs)
	}
}

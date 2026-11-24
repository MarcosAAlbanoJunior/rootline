CREATE TABLE IF NOT EXISTS customers (
    id   BIGINT PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS orders (
    id          BIGINT PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers (id),
    placed_at   TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS order_items (
    id       BIGINT PRIMARY KEY,
    order_id BIGINT NOT NULL REFERENCES orders (id),
    sku      TEXT NOT NULL,
    qty      INT NOT NULL
);

-- The foreign keys are indexed: the base demo-app is healthy, and a missing-index scenario removes
-- or avoids an index on purpose (ADR-0023).
CREATE INDEX IF NOT EXISTS orders_customer_id_idx ON orders (customer_id);
CREATE INDEX IF NOT EXISTS order_items_order_id_idx ON order_items (order_id);

-- Deterministic seed "deterministic-sql-v1": 20 customers, 10 orders each, 3 items per order.
INSERT INTO customers (id, name)
SELECT c, 'customer-' || c FROM generate_series(1, 20) AS c
ON CONFLICT DO NOTHING;

INSERT INTO orders (id, customer_id, placed_at)
SELECT (c - 1) * 10 + o, c, TIMESTAMPTZ '2026-01-01T00:00:00Z' + make_interval(hours => (c - 1) * 10 + o)
FROM generate_series(1, 20) AS c, generate_series(1, 10) AS o
ON CONFLICT DO NOTHING;

INSERT INTO order_items (id, order_id, sku, qty)
SELECT (ord - 1) * 3 + i, ord, 'sku-' || i, i
FROM generate_series(1, 200) AS ord, generate_series(1, 3) AS i
ON CONFLICT DO NOTHING;

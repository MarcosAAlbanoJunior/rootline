CREATE TABLE customers (id bigint PRIMARY KEY, name text NOT NULL);
CREATE TABLE orders (id bigint PRIMARY KEY, customer_id bigint NOT NULL REFERENCES customers(id), total_cents bigint NOT NULL);
CREATE TABLE order_items (id bigint PRIMARY KEY, order_id bigint NOT NULL REFERENCES orders(id), sku text NOT NULL, qty int NOT NULL);
CREATE INDEX ON orders (customer_id);
CREATE INDEX ON order_items (order_id);
-- seed fixo: 20 clientes, 10 pedidos cada, 3 itens por pedido
INSERT INTO customers SELECT g, 'customer-' || g FROM generate_series(1, 20) g;
INSERT INTO orders SELECT g, ((g - 1) / 10) + 1, g * 100 FROM generate_series(1, 200) g;
INSERT INTO order_items SELECT g, ((g - 1) / 3) + 1, 'sku-' || (g % 7), (g % 5) + 1 FROM generate_series(1, 600) g;
ANALYZE;

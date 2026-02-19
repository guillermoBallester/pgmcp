-- Demo database: a small e-commerce schema for testing Isthmus

CREATE TABLE customers (
    id         SERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    email      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE customers IS 'Registered customers';
COMMENT ON COLUMN customers.email IS 'Unique email address used for login';

CREATE TABLE products (
    id          SERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    price       NUMERIC(10,2) NOT NULL,
    stock       INTEGER NOT NULL DEFAULT 0
);
COMMENT ON TABLE products IS 'Product catalog';
COMMENT ON COLUMN products.price IS 'Price in USD';

CREATE TABLE orders (
    id          SERIAL PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'shipped', 'delivered', 'cancelled')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE orders IS 'Customer orders';
CREATE INDEX idx_orders_customer ON orders(customer_id);
CREATE INDEX idx_orders_status ON orders(status);

CREATE TABLE order_items (
    id         SERIAL PRIMARY KEY,
    order_id   INTEGER NOT NULL REFERENCES orders(id),
    product_id INTEGER NOT NULL REFERENCES products(id),
    quantity   INTEGER NOT NULL DEFAULT 1,
    unit_price NUMERIC(10,2) NOT NULL
);
COMMENT ON TABLE order_items IS 'Line items within an order';
CREATE INDEX idx_order_items_order ON order_items(order_id);

-- Seed data

INSERT INTO customers (name, email) VALUES
    ('Alice Johnson', 'alice@example.com'),
    ('Bob Smith', 'bob@example.com'),
    ('Carol Williams', 'carol@example.com'),
    ('Dave Brown', 'dave@example.com'),
    ('Eve Davis', 'eve@example.com');

INSERT INTO products (name, description, price, stock) VALUES
    ('Mechanical Keyboard', 'Cherry MX Brown switches, TKL layout', 129.99, 45),
    ('Wireless Mouse', 'Ergonomic, 2.4GHz + Bluetooth', 49.99, 120),
    ('USB-C Hub', '7-in-1: HDMI, USB-A x3, SD, microSD, PD', 39.99, 200),
    ('Monitor Stand', 'Adjustable height, bamboo + aluminum', 79.99, 30),
    ('Webcam HD', '1080p, auto-focus, built-in mic', 69.99, 85),
    ('Desk Lamp', 'LED, adjustable color temperature', 34.99, 60),
    ('Cable Management Kit', 'Clips, sleeves, and ties bundle', 19.99, 300);

INSERT INTO orders (customer_id, status, created_at) VALUES
    (1, 'delivered', now() - interval '30 days'),
    (1, 'shipped',   now() - interval '3 days'),
    (2, 'delivered', now() - interval '20 days'),
    (3, 'pending',   now() - interval '1 day'),
    (4, 'cancelled', now() - interval '15 days'),
    (5, 'delivered', now() - interval '10 days'),
    (5, 'shipped',   now() - interval '2 days'),
    (2, 'pending',   now());

INSERT INTO order_items (order_id, product_id, quantity, unit_price) VALUES
    (1, 1, 1, 129.99),
    (1, 3, 2,  39.99),
    (2, 2, 1,  49.99),
    (2, 5, 1,  69.99),
    (3, 4, 1,  79.99),
    (3, 6, 2,  34.99),
    (4, 1, 1, 129.99),
    (4, 7, 3,  19.99),
    (5, 2, 1,  49.99),
    (6, 3, 1,  39.99),
    (6, 5, 1,  69.99),
    (6, 6, 1,  34.99),
    (7, 1, 1, 129.99),
    (7, 4, 1,  79.99),
    (8, 7, 5,  19.99);

ANALYZE;

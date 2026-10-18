DROP SCHEMA IF EXISTS shop CASCADE;
CREATE SCHEMA shop;
SET LOCAL search_path = shop;

-- Categories reference themselves: a self join finds each one's parent.
CREATE TABLE categories (
  id        integer PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  name      text NOT NULL UNIQUE,
  parent_id integer REFERENCES categories (id)
);

CREATE TABLE customers (
  id      integer PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  name    text NOT NULL,
  email   text UNIQUE,
  city    text,
  joined  date NOT NULL DEFAULT current_date
);

CREATE TABLE products (
  id          integer PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  name        text NOT NULL,
  price       numeric(10, 2) NOT NULL CHECK (price >= 0),
  category_id integer REFERENCES categories (id) ON DELETE SET NULL
);

-- One customer has many orders.
CREATE TABLE orders (
  id          integer PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  customer_id integer NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
  ordered_at  date NOT NULL,
  status      text NOT NULL DEFAULT 'pending'
);

-- Orders and products are many-to-many through this join table.
CREATE TABLE order_items (
  order_id   integer REFERENCES orders (id) ON DELETE CASCADE,
  product_id integer REFERENCES products (id),
  quantity   integer NOT NULL CHECK (quantity > 0),
  PRIMARY KEY (order_id, product_id)
);

INSERT INTO categories (name, parent_id) VALUES
  ('Electronics', NULL), ('Books', NULL), ('Phones', 1), ('Laptops', 1), ('Fiction', 2);

INSERT INTO customers (name, email, city, joined) VALUES
  ('Asha',   'asha@example.com',   'Raipur', '2025-01-10'),
  ('Bilal',  'bilal@example.com',  'Pune',   '2025-03-02'),
  ('Chen',   NULL,                 'Raipur', '2025-04-18'),
  ('Divya',  'divya@example.com',  NULL,     '2025-06-30'),
  ('Elena',  'elena@example.com',  'Delhi',  '2025-08-14');  -- never ordered

INSERT INTO products (name, price, category_id) VALUES
  ('Pixel 9',        64999.00, 3),
  ('ThinkPad X1',   129999.00, 4),
  ('USB-C cable',      499.00, 1),
  ('Dune',             399.00, 5),
  ('SQL for Mortals',  899.00, 2),
  ('Mystery box',       99.00, NULL); -- no category, never ordered

INSERT INTO orders (customer_id, ordered_at, status) VALUES
  (1, '2025-02-01', 'shipped'),
  (1, '2025-05-20', 'delivered'),
  (2, '2025-03-15', 'delivered'),
  (3, '2025-07-04', 'pending'),
  (4, '2025-09-09', 'cancelled'),
  (2, '2025-10-01', 'pending');  -- empty order: no items

INSERT INTO order_items (order_id, product_id, quantity) VALUES
  (1, 1, 1), (1, 3, 2),
  (2, 4, 1), (2, 5, 1),
  (3, 2, 1),
  (4, 3, 3), (4, 4, 2),
  (5, 5, 1);

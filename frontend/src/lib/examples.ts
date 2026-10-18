// Starter queries for the sample "shop" schema, one per concept.
export const examples: { group: string; title: string; sql: string }[] = [
  { group: "Basics", title: "Filter and sort", sql: "SELECT name, city\nFROM customers\nWHERE city = 'Raipur'\nORDER BY name;" },
  { group: "Basics", title: "NULL is not = NULL", sql: "SELECT name, email\nFROM customers\nWHERE email IS NULL;" },
  {
    group: "Joins",
    title: "INNER JOIN",
    sql: "SELECT c.name, o.id AS order_id, o.status\nFROM customers c\nJOIN orders o ON o.customer_id = c.id;",
  },
  {
    group: "Joins",
    title: "LEFT JOIN (keep everyone)",
    sql: "SELECT c.name, o.id AS order_id, o.status\nFROM customers c\nLEFT JOIN orders o ON o.customer_id = c.id;",
  },
  {
    group: "Joins",
    title: "LEFT JOIN + WHERE trap",
    sql: "SELECT c.name, o.status\nFROM customers c\nLEFT JOIN orders o ON o.customer_id = c.id\nWHERE o.status = 'pending';",
  },
  {
    group: "Joins",
    title: "Anti-join: never ordered",
    sql: "SELECT c.name\nFROM customers c\nLEFT JOIN orders o ON o.customer_id = c.id\nWHERE o.id IS NULL;",
  },
  {
    group: "Joins",
    title: "FULL JOIN",
    sql: "SELECT p.name AS product, c.name AS category\nFROM products p\nFULL JOIN categories c ON c.id = p.category_id;",
  },
  {
    group: "Joins",
    title: "Many-to-many (3 tables)",
    sql: "SELECT o.id AS order_id, p.name, oi.quantity\nFROM orders o\nJOIN order_items oi ON oi.order_id = o.id\nJOIN products p ON p.id = oi.product_id;",
  },
  {
    group: "Joins",
    title: "Self join (parent category)",
    sql: "SELECT child.name, parent.name AS parent\nFROM categories child\nLEFT JOIN categories parent ON parent.id = child.parent_id;",
  },
  { group: "Joins", title: "CROSS JOIN", sql: "SELECT c.name, p.name\nFROM customers c\nCROSS JOIN products p;" },
  {
    group: "Grouping",
    title: "GROUP BY + HAVING",
    sql: "SELECT c.name, count(o.id) AS orders\nFROM customers c\nLEFT JOIN orders o ON o.customer_id = c.id\nGROUP BY c.name\nHAVING count(o.id) >= 1\nORDER BY orders DESC;",
  },
  {
    group: "Grouping",
    title: "Revenue per order",
    sql: "SELECT oi.order_id, sum(p.price * oi.quantity) AS total\nFROM order_items oi\nJOIN products p ON p.id = oi.product_id\nGROUP BY oi.order_id\nORDER BY total DESC\nLIMIT 3;",
  },
  { group: "Writes", title: "INSERT (FK ok)", sql: "INSERT INTO orders (customer_id, ordered_at)\nVALUES (5, current_date);" },
  { group: "Writes", title: "INSERT (FK fails)", sql: "INSERT INTO orders (customer_id, ordered_at)\nVALUES (99, current_date);" },
  { group: "Writes", title: "DELETE with CASCADE", sql: "DELETE FROM customers\nWHERE id = 1;" },
  { group: "Writes", title: "DELETE blocked by FK", sql: "DELETE FROM products\nWHERE id = 1;" },
  { group: "Writes", title: "UPDATE", sql: "UPDATE orders\nSET status = 'delivered'\nWHERE status = 'pending';" },
  {
    group: "Schema",
    title: "CREATE TABLE with FK",
    sql: "CREATE TABLE reviews (\n  id integer PRIMARY KEY GENERATED ALWAYS AS IDENTITY,\n  product_id integer NOT NULL REFERENCES products (id),\n  customer_id integer REFERENCES customers (id),\n  stars integer CHECK (stars BETWEEN 1 AND 5)\n);",
  },
];

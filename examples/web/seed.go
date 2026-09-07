package main

var seed = []string{
	`CREATE TABLE customers (id int PRIMARY KEY, name text NOT NULL, city text NOT NULL)`,
	`CREATE TABLE products (id int PRIMARY KEY, name text NOT NULL, category text NOT NULL, price int NOT NULL CHECK(price >= 0))`,
	`CREATE TABLE orders (id int PRIMARY KEY, customer_id int REFERENCES customers(id), product_id int REFERENCES products(id), quantity int NOT NULL CHECK(quantity > 0))`,
	`INSERT INTO customers VALUES (1,'Alice','Seoul'),(2,'Bob','Busan'),(3,'Charlie','Jeju')`,
	`INSERT INTO products VALUES (1,'Mechanical keyboard','Desk',129000),(2,'Desk lamp','Desk',48000),(3,'Canvas tote','Everyday',24000),(4,'Travel mug','Everyday',32000),(5,'Notebook set','Stationery',18000),(6,'Monitor stand','Desk',65000)`,
	`INSERT INTO orders VALUES (1,1,1,1),(2,1,4,2),(3,2,2,1),(4,3,3,2),(5,2,5,3)`,
}

const schemaQuery = `SELECT c.relname, c.relkind::text, a.attname, t.typname
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped
LEFT JOIN pg_catalog.pg_type t ON t.oid=a.atttypid
WHERE n.nspname=current_schema() AND c.relkind IN ('r','p','v','m')
ORDER BY c.relname,a.attnum`

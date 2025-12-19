package store

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB Schema
const schema = `
CREATE TABLE IF NOT EXISTS positions (
	token_id TEXT PRIMARY KEY,
	amount REAL,
	avg_price REAL,
	updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS orders (
	id TEXT PRIMARY KEY,
	client_order_id TEXT,
	token_id TEXT,
	side TEXT,
	price REAL,
	size REAL,
	filled_size REAL,
	status TEXT, -- NEW, PARTIAL, FILLED, CANCELED
	created_at DATETIME,
	updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS system_state (
	key TEXT PRIMARY KEY,
	value TEXT,
	updated_at DATETIME
);
`

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) *Store {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}

	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("Failed to init schema: %v", err)
	}

	return &Store{db: db}
}

// Position Methods
func (s *Store) UpdatePosition(tokenID string, amount, price float64) error {
	// Simple average price calculation logic usually happens in business logic, 
	// here we just upsert current state.
	query := `
	INSERT INTO positions (token_id, amount, avg_price, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(token_id) DO UPDATE SET
		amount = excluded.amount,
		avg_price = excluded.avg_price,
		updated_at = excluded.updated_at;
	`
	_, err := s.db.Exec(query, tokenID, amount, price, time.Now())
	return err
}

func (s *Store) GetPosition(tokenID string) (float64, float64, error) {
	var amount, avgPrice float64
	err := s.db.QueryRow("SELECT amount, avg_price FROM positions WHERE token_id = ?", tokenID).Scan(&amount, &avgPrice)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return amount, avgPrice, err
}

// Order Methods
func (s *Store) SaveOrder(orderID, clientID, tokenID, side, status string, price, size float64) error {
	query := `
	INSERT INTO orders (id, client_order_id, token_id, side, price, size, filled_size, status, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
	`
	_, err := s.db.Exec(query, orderID, clientID, tokenID, side, price, size, status, time.Now(), time.Now())
	return err
}

func (s *Store) UpdateOrderStatus(orderID, status string, filledSize float64) error {
	query := `
	UPDATE orders SET status = ?, filled_size = ?, updated_at = ? WHERE id = ?
	`
	_, err := s.db.Exec(query, status, filledSize, time.Now(), orderID)
	return err
}

func (s *Store) GetOpenOrders() ([]map[string]interface{}, error) {
	rows, err := s.db.Query("SELECT id, token_id, side, price, size, filled_size FROM orders WHERE status IN ('NEW', 'PARTIAL')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []map[string]interface{}
	for rows.Next() {
		var id, tokenID, side string
		var price, size, filled float64
		if err := rows.Scan(&id, &tokenID, &side, &price, &size, &filled); err != nil {
			continue
		}
		orders = append(orders, map[string]interface{}{
			"id": id, "token_id": tokenID, "side": side, "price": price, "size": size, "filled": filled,
		})
	}
	return orders, nil
}

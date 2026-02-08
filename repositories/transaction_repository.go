package repositories

import (
	"database/sql"
	"fmt"
	"kasir-api/models"
)

type TransactionRepository struct {
	db *sql.DB
}

func NewTransactionRepository(db *sql.DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

func (repo *TransactionRepository) CreateTransaction(items []models.CheckoutItem) (*models.Transaction, error) {
	tx, err := repo.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	totalAmount := 0
	details := make([]models.TransactionDetail, 0)

	for _, item := range items {
		var productPrice, stock int
		var productName string

		err := tx.QueryRow("SELECT name, price, stock FROM products WHERE id = $1", item.ProductID).Scan(&productName, &productPrice, &stock)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("product id %d not found", item.ProductID)
		}
		if err != nil {
			return nil, err
		}

		subtotal := productPrice * item.Quantity
		totalAmount += subtotal

		_, err = tx.Exec("UPDATE products SET stock = stock - $1 WHERE id = $2", item.Quantity, item.ProductID)
		if err != nil {
			return nil, err
		}

		details = append(details, models.TransactionDetail{
			ProductID:   item.ProductID,
			ProductName: productName,
			Quantity:    item.Quantity,
			Subtotal:    subtotal,
		})
	}

	var transactionID int
	err = tx.QueryRow("INSERT INTO transactions (total_amount) VALUES ($1) RETURNING id", totalAmount).Scan(&transactionID)
	if err != nil {
		return nil, err
	}

	var transactionDetailsID int
	for i := range details {

		err = tx.QueryRow("INSERT INTO transaction_details (transaction_id, product_id, quantity, subtotal) VALUES ($1, $2, $3, $4) RETURNING id",
			transactionID, details[i].ProductID, details[i].Quantity, details[i].Subtotal).Scan(&transactionDetailsID)
		if err != nil {
			return nil, err
		}
		details[i].ID = transactionDetailsID
		details[i].TransactionID = transactionID
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &models.Transaction{
		ID:          transactionID,
		TotalAmount: totalAmount,
		Details:     details,
	}, nil
}

func (repo *TransactionRepository) LaporanHariIni() (*models.RevenueSummary, error) {
	// 1) Total revenue & total transaksi hari ini
	var totalRevenue sql.NullInt64
	var totalTransaksi sql.NullInt64

	err := repo.db.QueryRow(`
		SELECT
			COALESCE(SUM(total_amount), 0) AS total_revenue,
			COALESCE(COUNT(*), 0)          AS total_transaksi
		FROM transactions
		WHERE created_at >= CURRENT_DATE
		  AND created_at <  CURRENT_DATE + INTERVAL '1 day'
	`).Scan(&totalRevenue, &totalTransaksi)
	if err != nil {
		return nil, err
	}

	// 2) Produk terlaris hari ini (berdasarkan total qty terjual)
	// Jika belum ada transaksi hari ini, query ini bisa "no rows" -> handle jadi default.
	var nama sql.NullString
	var qty sql.NullInt64

	err = repo.db.QueryRow(`
		SELECT
			p.name AS nama,
			COALESCE(SUM(td.quantity), 0) AS qty_terjual
		FROM transaction_details td
		JOIN transactions t ON t.id = td.transaction_id
		JOIN products p     ON p.id = td.product_id
		WHERE t.created_at >= CURRENT_DATE
		  AND t.created_at <  CURRENT_DATE + INTERVAL '1 day'
		GROUP BY p.id, p.name
		ORDER BY qty_terjual DESC, p.name ASC
		LIMIT 1
	`).Scan(&nama, &qty)

	produkTerlaris := models.ProdukTerlaris{
		Nama:       "",
		QtyTerjual: 0,
	}

	if err != nil {
		if err != sql.ErrNoRows {
			return nil, err
		}
		// no rows -> biarkan default kosong
	} else {
		if nama.Valid {
			produkTerlaris.Nama = nama.String
		}
		if qty.Valid {
			produkTerlaris.QtyTerjual = int(qty.Int64)
		}
	}

	return &models.RevenueSummary{
		TotalRevenue:   int(totalRevenue.Int64),
		TotalTransaksi: int(totalTransaksi.Int64),
		ProdukTerlaris: produkTerlaris,
	}, nil
}

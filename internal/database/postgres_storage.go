package database

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Luckyx-Labs/chainscan-sync/internal/interfaces"
	appLogger "github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
)

// PostgresStorage PostgreSQL storage implementation
type PostgresStorage struct {
	db        *gorm.DB // Main database (chain scan data)
	accountDB *gorm.DB // Account database (monitoring addresses)
}

// NewPostgresStorage creates a new PostgreSQL storage instance
func NewPostgresStorage(dsn string, dsn2 ...string) (interfaces.Storage, error) {
	// Configure GORM - main database
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info), // Set to Info level to print SQL
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL (main): %w", err)
	}

	// Get underlying sql.DB to configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection (main): %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Configure GORM - account database (monitoring addresses)
	var accountDB *gorm.DB
	accountDSN := dsn // Default to main database
	if len(dsn2) > 0 && dsn2[0] != "" {
		accountDSN = dsn2[0]
	}

	accountDB, err = gorm.Open(postgres.Open(accountDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL (account): %w", err)
	}

	// Configure account database connection pool
	accountSQLDB, err := accountDB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection (account): %w", err)
	}

	accountSQLDB.SetMaxOpenConns(10) // Account database can have fewer connections
	accountSQLDB.SetMaxIdleConns(3)
	accountSQLDB.SetConnMaxLifetime(5 * time.Minute)

	storage := &PostgresStorage{
		db:        db,
		accountDB: accountDB,
	}

	// Auto migrate database schema (main database only)
	if err := storage.AutoMigrate(); err != nil {
		return nil, fmt.Errorf("failed to auto migrate database: %w", err)
	}

	if accountDSN == dsn {
		appLogger.Info("PostgreSQL storage initialized successfully (single database mode)")
	} else {
		appLogger.Info("PostgreSQL storage initialized successfully (dual database mode: main + account)")
	}
	return storage, nil
}

// AutoMigrate auto migrates database schema
func (s *PostgresStorage) AutoMigrate() error {
	// Ensure blockscan schema exists
	if err := s.db.Exec("CREATE SCHEMA IF NOT EXISTS blockscan").Error; err != nil {
		return fmt.Errorf("failed to create blockscan schema: %w", err)
	}

	// Auto create/update table schema
	err := s.db.AutoMigrate(
		&Event{},
		&Transaction{},
		&AddressBalance{},
		&BalanceHistory{},
		&Block{},
		&Withdraw{},
		&Deposit{},
	)

	if err != nil {
		return fmt.Errorf("failed to auto migrate database: %w", err)
	}

	// Create composite indexes
	s.db.Exec("CREATE INDEX IF NOT EXISTS idx_events_chain_processed ON events(chain_name, processed)")
	s.db.Exec("CREATE INDEX IF NOT EXISTS idx_events_contract_name ON events(contract_address, event_name)")
	s.db.Exec("CREATE INDEX IF NOT EXISTS idx_transactions_from_to ON transactions(from_address, to_address)")
	s.db.Exec("CREATE INDEX IF NOT EXISTS idx_transactions_block ON transactions(chain_name, block_number)")
	s.db.Exec("CREATE INDEX IF NOT EXISTS idx_balance_histories_address_time ON balance_histories(address, timestamp)")

	// Confirmation count update optimization index (partial index, only indexes unconfirmed events)
	s.db.Exec("CREATE INDEX IF NOT EXISTS idx_events_unfinalized ON blockscan.events(block_number) WHERE is_finalized = false")

	appLogger.Info("Database schema migration completed")
	return nil
}

// GetDB gets the database instance (for advanced operations)
func (s *PostgresStorage) GetDB() *gorm.DB {
	return s.db
}

func (s *PostgresStorage) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}

	if s.accountDB != nil {
		if sqlDB, err := s.accountDB.DB(); err == nil {
			sqlDB.Close()
		}
	}

	appLogger.Info("Closing PostgreSQL connection")
	return sqlDB.Close()
}

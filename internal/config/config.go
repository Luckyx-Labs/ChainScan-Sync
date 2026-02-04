package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config global configuration structure
type Config struct {
	Log         LogConfig         `mapstructure:"log"`
	Database    DatabaseConfig    `mapstructure:"database"`
	API         APIConfig         `mapstructure:"api"`
	EVMChains   []EVMChainConfig  `mapstructure:"evm_chains"`
	Retry       RetryConfig       `mapstructure:"retry"`
	Performance PerformanceConfig `mapstructure:"performance"`
	Webhooks    WebhooksConfig    `mapstructure:"webhooks"`
	WithdrawEOA WithdrawEOAConfig `mapstructure:"withdraw_eoa"`
}

// WithdrawEOAConfig withdraw EOA address configuration
type WithdrawEOAConfig struct {
	Address string `mapstructure:"address"`
}

// LogConfig log configuration
type LogConfig struct {
	Level    string `mapstructure:"level"`
	Format   string `mapstructure:"format"`
	Output   string `mapstructure:"output"`
	FilePath string `mapstructure:"file_path"`
}

// DatabaseConfig database configuration
type DatabaseConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Driver  string `mapstructure:"driver"`
	DSN     string `mapstructure:"dsn"`  // Primary database (chain scanning data)
	DSN2    string `mapstructure:"dsn2"` // Secondary database (monitoring addresses)
}

// EVMChainConfig EVM chain configuration
type EVMChainConfig struct {
	Name          string           `mapstructure:"name"`
	Enabled       bool             `mapstructure:"enabled"`
	ChainID       uint64           `mapstructure:"chain_id"`
	RPCURL        string           `mapstructure:"rpc_url"`         // Primary RPC endpoint
	BackupRPCURLs []string         `mapstructure:"backup_rpc_urls"` // Backup RPC endpoints
	RateLimit     float64          `mapstructure:"rate_limit"`      // RPC request rate limit (requests/second), default 30
	WSURL         string           `mapstructure:"ws_url"`
	StartBlock    string           `mapstructure:"start_block"`
	Confirmations uint64           `mapstructure:"confirmations"`
	Contracts     []ContractConfig `mapstructure:"contracts"`
}

// ContractConfig contract configuration
type ContractConfig struct {
	Address string   `mapstructure:"address"`
	Name    string   `mapstructure:"name"`
	ABIPath string   `mapstructure:"abi_path"`
	Events  []string `mapstructure:"events"`
}

// RetryConfig retry configuration
type RetryConfig struct {
	MaxAttempts     int           `mapstructure:"max_attempts"`
	InitialInterval time.Duration `mapstructure:"initial_interval"`
	MaxInterval     time.Duration `mapstructure:"max_interval"`
	Multiplier      float64       `mapstructure:"multiplier"`
}

// PerformanceConfig performance configuration
type PerformanceConfig struct {
	BatchSize    int           `mapstructure:"batch_size"`
	WorkerCount  int           `mapstructure:"worker_count"`
	BufferSize   int           `mapstructure:"buffer_size"`
	PollInterval time.Duration `mapstructure:"poll_interval"`
}

// APIConfig API service configuration
type APIConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Host      string `mapstructure:"host"`
	Port      int    `mapstructure:"port"`
	AuthToken string `mapstructure:"auth_token"` // API authentication token
}

// WebhooksConfig Webhook configuration
type WebhooksConfig struct {
	Enabled   bool              `mapstructure:"enabled"`
	Endpoints []WebhookEndpoint `mapstructure:"endpoints"`
}

// WebhookEndpoint Webhook endpoint configuration
type WebhookEndpoint struct {
	URL    string `mapstructure:"url"`
	APIKey string `mapstructure:"api_key"`
	Retry  bool   `mapstructure:"retry"`
}

// LoadConfig loads configuration
func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// Set default values
	setDefaults()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config file: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	return &config, nil
}

// setDefaults sets default values
func setDefaults() {
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "json")
	viper.SetDefault("log.output", "stdout")
	viper.SetDefault("api.enabled", true)
	viper.SetDefault("api.host", "0.0.0.0")
	viper.SetDefault("api.port", 8080)
	viper.SetDefault("retry.max_attempts", 3)
	viper.SetDefault("retry.initial_interval", "1s")
	viper.SetDefault("retry.max_interval", "30s")
	viper.SetDefault("retry.multiplier", 2.0)
	viper.SetDefault("performance.batch_size", 100)
	viper.SetDefault("performance.worker_count", 4)
	viper.SetDefault("performance.buffer_size", 1000)
	viper.SetDefault("performance.poll_interval", "3s")
}

// Validate verifies the configuration values
func (c *Config) Validate() error {
	// 1. verify log configuration
	if err := c.validateLog(); err != nil {
		return fmt.Errorf("failed to validate log config: %w", err)
	}

	// 2. verify database configuration
	if err := c.validateDatabase(); err != nil {
		return fmt.Errorf("failed to validate database config: %w", err)
	}

	// 3. verify at least one chain is enabled
	enabledChains := 0
	for _, chain := range c.EVMChains {
		if chain.Enabled {
			enabledChains++
		}
	}
	if enabledChains == 0 {
		return fmt.Errorf("need to enable at least one blockchain")
	}

	// 4. verify EVM chain configuration
	for i, chain := range c.EVMChains {
		if chain.Enabled {
			if err := c.validateEVMChain(i, &chain); err != nil {
				return fmt.Errorf("failed to validate EVM chain[%d](%s) config: %w", i, chain.Name, err)
			}
		}
	}

	// 5. verify retry configuration
	if err := c.validateRetry(); err != nil {
		return fmt.Errorf("failed to validate retry config: %w", err)
	}

	// 6. verify performance configuration
	if err := c.validatePerformance(); err != nil {
		return fmt.Errorf("failed to validate performance config: %w", err)
	}

	// 7. verify webhook configuration
	if err := c.validateWebhooks(); err != nil {
		return fmt.Errorf("failed to validate webhook config: %w", err)
	}

	return nil
}

// validateLog verifies the log configuration
func (c *Config) validateLog() error {
	validLevels := []string{"debug", "info", "warn", "error"}
	if !contains(validLevels, c.Log.Level) {
		return fmt.Errorf("invalid log level: %s, valid values: %v", c.Log.Level, validLevels)
	}

	validFormats := []string{"json", "text"}
	if !contains(validFormats, c.Log.Format) {
		return fmt.Errorf("invalid log format: %s, valid values: %v", c.Log.Format, validFormats)
	}

	validOutputs := []string{"stdout", "file", "both"}
	if !contains(validOutputs, c.Log.Output) {
		return fmt.Errorf("invalid log output: %s, valid values: %v", c.Log.Output, validOutputs)
	}

	if (c.Log.Output == "file" || c.Log.Output == "both") && c.Log.FilePath == "" {
		return fmt.Errorf("log output is file or both, file_path must be specified")
	}

	return nil
}

// validateDatabase verifies the database configuration
func (c *Config) validateDatabase() error {
	if !c.Database.Enabled {
		return nil
	}

	if c.Database.Driver == "" {
		return fmt.Errorf("database driver cannot be empty")
	}

	if c.Database.Driver != "postgres" {
		return fmt.Errorf("unsupported database driver: %s, currently only postgres is supported", c.Database.Driver)
	}

	if c.Database.DSN == "" {
		return fmt.Errorf("database DSN cannot be empty")
	}

	// verify PostgreSQL DSN format
	requiredFields := []string{"host=", "port=", "user=", "dbname="}
	for _, field := range requiredFields {
		if !strings.Contains(c.Database.DSN, field) {
			return fmt.Errorf("database DSN missing required field: %s", strings.TrimSuffix(field, "="))
		}
	}

	return nil
}

// validateEVMChain verifies the EVM chain configuration
func (c *Config) validateEVMChain(index int, chain *EVMChainConfig) error {
	if chain.Name == "" {
		return fmt.Errorf("chain name cannot be empty")
	}

	if chain.ChainID == 0 {
		return fmt.Errorf("chain_id cannot be 0")
	}

	if chain.RPCURL == "" {
		return fmt.Errorf("rpc_url cannot be empty")
	}

	// verify RPC URL format
	if err := validateURL(chain.RPCURL); err != nil {
		return fmt.Errorf("invalid rpc_url: %w", err)
	}

	// verify WebSocket URL format (if provided)
	if chain.WSURL != "" {
		if err := validateURL(chain.WSURL); err != nil {
			return fmt.Errorf("invalid ws_url: %w", err)
		}
	}

	// verify start block
	if chain.StartBlock != "" && chain.StartBlock != "latest" && chain.StartBlock != "earliest" {
		// check if it's a valid number
		if _, err := strconv.ParseUint(chain.StartBlock, 10, 64); err != nil {
			return fmt.Errorf("invalid start_block: %s, must be 'latest', 'earliest', or a specific block number", chain.StartBlock)
		}
	}

	// verify contract configuration
	if len(chain.Contracts) == 0 {
		return fmt.Errorf("at least one contract must be configured")
	}

	for j, contract := range chain.Contracts {
		if err := c.validateContract(j, &contract); err != nil {
			return fmt.Errorf("contract[%d] validation failed: %w", j, err)
		}
	}

	return nil
}

// validateContract verifies the contract configuration
func (c *Config) validateContract(index int, contract *ContractConfig) error {
	if contract.Address == "" {
		return fmt.Errorf("contract address cannot be empty")
	}

	// verify Ethereum address format (0x + 40 hexadecimal characters)
	ethAddressRegex := regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
	if !ethAddressRegex.MatchString(contract.Address) {
		return fmt.Errorf("invalid contract address format: %s", contract.Address)
	}

	if contract.Name == "" {
		return fmt.Errorf("contract name cannot be empty")
	}

	if contract.ABIPath == "" {
		return fmt.Errorf("ABI path cannot be empty")
	}

	// verify if ABI file exists
	if _, err := os.Stat(contract.ABIPath); os.IsNotExist(err) {
		return fmt.Errorf("ABI file does not exist: %s", contract.ABIPath)
	}

	if len(contract.Events) == 0 {
		return fmt.Errorf("at least one event must be configured")
	}

	return nil
}

// validateRetry verifies the retry configuration
func (c *Config) validateRetry() error {
	if c.Retry.MaxAttempts < 0 {
		return fmt.Errorf("max_attempts cannot be negative")
	}

	if c.Retry.InitialInterval < 0 {
		return fmt.Errorf("initial_interval cannot be negative")
	}

	if c.Retry.MaxInterval < 0 {
		return fmt.Errorf("max_interval cannot be negative")
	}

	if c.Retry.MaxInterval < c.Retry.InitialInterval {
		return fmt.Errorf("max_interval cannot be less than initial_interval")
	}

	if c.Retry.Multiplier < 1.0 {
		return fmt.Errorf("multiplier cannot be less than 1.0")
	}

	return nil
}

// validatePerformance verifies the performance configuration
func (c *Config) validatePerformance() error {
	if c.Performance.BatchSize <= 0 {
		return fmt.Errorf("batch_size must be greater than 0")
	}

	if c.Performance.WorkerCount <= 0 {
		return fmt.Errorf("worker_count must be greater than 0")
	}

	if c.Performance.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be greater than 0")
	}

	if c.Performance.PollInterval <= 0 {
		return fmt.Errorf("poll_interval must be greater than 0")
	}

	return nil
}

// validateWebhooks verifies the webhook configuration
func (c *Config) validateWebhooks() error {
	if !c.Webhooks.Enabled {
		return nil
	}

	if len(c.Webhooks.Endpoints) == 0 {
		return fmt.Errorf("when webhooks are enabled, at least one endpoint must be configured")
	}

	for i, endpoint := range c.Webhooks.Endpoints {
		if endpoint.URL == "" {
			return fmt.Errorf("webhook endpoint[%d] URL cannot be empty", i)
		}

		if err := validateURL(endpoint.URL); err != nil {
			return fmt.Errorf("webhook endpoint[%d] URL is invalid: %w", i, err)
		}
	}

	return nil
}

// validateURL verifies the URL format
func validateURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL parsing failed: %w", err)
	}

	if parsedURL.Scheme == "" {
		return fmt.Errorf("URL is missing scheme (http/https/ws/wss)")
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("URL is missing host")
	}

	return nil
}

// contains checks if a string slice contains a specified string
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

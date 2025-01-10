package config

import (
	"fmt"

	"github.com/222-crypto/blockdump/v2/rpc"
)

// ValidateRPCConfig performs validation on the RPC configuration.
func ValidateRPCConfig(config *rpc.RPCConfig) error {
	// Validate RPCConfig
	if config.Port <= 0 {
		return fmt.Errorf("invalid RPC port: %d", config.Port)
	}
	if config.ClientTimeout <= 0 {
		return fmt.Errorf("invalid RPC client timeout: %v", config.ClientTimeout)
	}
	if config.RetryLimit < 0 {
		return fmt.Errorf("invalid RPC retry limit: %d", config.RetryLimit)
	}
	if config.RetryBackoffMin <= 0 {
		return fmt.Errorf("invalid minimum retry backoff: %v", config.RetryBackoffMin)
	}
	if config.RetryBackoffMax < config.RetryBackoffMin {
		return fmt.Errorf("maximum retry backoff (%v) must be greater than minimum (%v)",
			config.RetryBackoffMax, config.RetryBackoffMin)
	}

	return nil
}

package rpc

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

type IConfig[T any] interface {
	// config ops
	BasicConfig() T                     // simple configuration with default values and no errors
	UpdateConfig(config func() T) error // returns an error if the config is invalid
}

type RPCConfig struct {
	Connect         string // IP or hostname of the node
	Port            int    // port of the node's RPC server
	User            string // RPC username
	Password        string // RPC password
	ClientTimeout   time.Duration
	RetryLimit      int
	RetryBackoffMax time.Duration
	RetryBackoffMin time.Duration
}

type IBasicBChainRPC interface {
	// configuration handling
	IConfig[RPCConfig]

	// raw ops
	GetBlockReader(ctx context.Context, blockhash string) (io.Reader, error)            // raw block data
	GetBlockJSON(ctx context.Context, blockhash string) (map[string]interface{}, error) // max verbosity JSON

	// lookup ops
	GetBlockhash(ctx context.Context, blockid int) (string, error)
	GetBestBlockhash(ctx context.Context) (string, error)
	GetBlockCount(ctx context.Context) (int, error)

	// validation ops
	IsValidBlockID(ctx context.Context, id int) (bool, error)
}

const BLOCKCHAIN_CLAM = "clam"

func DefaultRPCConfig(blockchain string) (rpc_config RPCConfig) {
	switch blockchain {

	case BLOCKCHAIN_CLAM:
		return RPCConfig{
			Connect:         "127.0.0.1",
			Port:            30174, // Default mainnet port
			User:            os.Getenv("BLOCKDUMP_RPC_USER"),
			Password:        os.Getenv("BLOCKDUMP_RPC_PASSWORD"),
			ClientTimeout:   900 * time.Second,
			RetryLimit:      3,
			RetryBackoffMax: 300 * time.Second,
			RetryBackoffMin: 1 * time.Second,
		}

	}

	panic(fmt.Errorf("Unknown blockchain: %s", blockchain))
}

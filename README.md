# `blockdump`

Blockdump is an application which provides a command line interface and Go library for dumping blocks from first-generation, RPC-enabled (e.g. Bitcoin) blockchains. It is designed to be fast and efficient, and can be used to dump blocks using standard golang interfaces or command line usage.

## Features

Blockdump offers several powerful capabilities for blockchain data extraction and analysis:

- Efficient block retrieval with streaming support (iter.Seq2)
- Flexible block range selection for targeted data extraction
- Command-line interface for easy integration into scripts and workflows
- Go library for programmatic access to raw blockchain data
- Built-in retry mechanism with exponential backoff for reliable operation
- Comprehensive error handling with detailed error context
- Configurable output formats (binary or text)

## Installation

### Prerequisites

- Go 1.23.4 or higher — 🍺 `brew install go`
- Access to an RPC-enabled blockchain (i.e. [CLAM](https://github.com/222-crypto/getclams)) node


### Installing from Source

```bash
# Clone the repository
git clone https://github.com/222-crypto/blockdump.git

# Change to the project directory
cd blockdump

# Build the project, local target: ./bin/blockdump
make build

# Optional: Install globally
go install
```


### Environment Setup

#### FYI
```
It is wise to have a dedicated node for block-data RPC access, distinct from your wallet or mining node.
```

Before using blockdump, set up your RPC credentials:

```bash
# Set your RPC credentials
export BLOCKDUMP_RPC_USER="your-username"
export BLOCKDUMP_RPC_PASSWORD="your-password"
```

_You also can supply these credentials via command line flags, [if you so choose](https://www.netmeister.org/blog/passing-passwords.html)._


## Quick Start

### Command Line Usage

_See `blockdump --help` for full CLI documentation._

Blockdump provides several commands for different block retrieval
operations. Output can be directed to files (-f flag) and
formatted as binary (-b flag) or hexadecimal text:

```bash
# Get a random block and output as text to stdout
blockdump randomblock

# Get a specific block by ID and save as text file
blockdump -f block222.txt specificblock 222

# Retrieve the genesis block and save as binary file
blockdump -fb genesis.bin genesisblock

# Retrieve a range of blocks in binary format
blockdump -fb blocks_over9k.bin blockrange 9000 9222

# Sample multiple random blocks and save as binary
blockdump -b -f sample222.bin randomblocksample 222

# Pipe binary output to another tool
blockdump -b genesisblock | xxd
```

### Library Usage

The Blockdump library provides two main ways to work with blocks:
 - By fetching them directly from a blockchain node.
 - By processing previously saved block data from files.

#### Fetching Blocks from Node

This example demonstrates how to connect to a CLAM node and retrieve blocks using secure credential handling:
```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/222-crypto/blockdump/v2/block"
	"github.com/222-crypto/blockdump/v2/rpc"
	"github.com/222-crypto/blockdump/v2/rpc/clamrpc"
)

const DEV_MODE = true

func main() {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Start with default configuration which automatically reads from environment variables:
	// - BLOCKDUMP_RPC_USER
	// - BLOCKDUMP_RPC_PASSWORD
	config := rpc.DefaultRPCConfig(rpc.BLOCKCHAIN_CLAM)

	// Optionally override non-sensitive defaults if needed
	config.Connect = "localhost"                     // Default is 127.0.0.1
	config.Port = 30174                              // Default CLAM mainnet port

	// For development/testing, you might want shorter timeouts
	if DEV_MODE {
		config.ClientTimeout = 5 * time.Second
		config.RetryBackoffMax = 3 * time.Second
	}

	// Initialize CLAM RPC client
	rpcClient, err := clamrpc.NewCLAMBasicBChainRPC(&config)
	if err != nil {
		log.Fatal("Failed to create RPC client:", err)
	}

	// Create block iterator
	iterator := block.NewBlockIterator(rpcClient)

	// Initialize BlockDump with the RPC client and iterator
	dumper, err := block.NewBlockDump(rpcClient, iterator)
	if err != nil {
		log.Fatal("Failed to create block dumper:", err)
	}

	// Example 1: Get the genesis block
	genesis, err := dumper.GenesisBlock(ctx)
	if err != nil {
		log.Fatal("Failed to get genesis block:", err)
	}
	fmt.Printf("Genesis block: %s, length=%d\n", genesis.Hash(), len(genesis.Bytes()))

	// Example 2: Get a range of blocks and process them
	start_block := 212
	end_block := 222
	blocks, err := dumper.BlockRange(ctx, start_block, end_block)
	if err != nil {
		log.Fatal("Failed to get block range:", err)
	}

	// Process blocks in the sequence
	for block, err := range blocks.Seq2() {
		if err != nil {
			log.Fatal("Failed to get block:", err)
		}
		fmt.Printf("Block [%d/%d] hash: %s, length=%d\n", block.ID(), end_block, block.Hash(), len(block.Bytes()))
	}

	// Example 3: Get a random sample of blocks
	sample, err := dumper.RandomBlockSample(ctx, 5)
	if err != nil {
		log.Fatal("Failed to get random sample:", err)
	}

	for block, err := range sample.Seq2() {
		if err != nil {
			log.Fatal("Failed to get block:", err)
		}

		fmt.Printf("Random block %d hash: %s, length=%d\n", block.ID(), block.Hash(), len(block.Bytes()))
	}
}
```

#### Processing Saved Block Data

This example shows how to work with previously saved block data files:
```go
# TODO
```

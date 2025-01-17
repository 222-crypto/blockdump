# `blockdump`

Blockdump is an application which provides a command line interface and Go library for dumping blocks from first-generation, RPC-enabled (e.g. Bitcoin) blockchains. It is designed to be fast and efficient, and can be used to dump blocks using standard golang interfaces or command line usage.

## Features

Blockdump offers several powerful capabilities for blockchain data extraction and analysis:

- Efficient block retrieval with streaming support (iter.Seq2)
- Flexible block range selection for targeted data extraction
- Command-line interface for easy integration into scripts and workflows
- Go library for programmatic access to blockchain data
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
```

#### Processing Saved Block Data

This example shows how to work with previously saved block data files:
```go
```

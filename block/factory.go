package block

import (
	"context"
	"fmt"
	"io"

	"github.com/222-crypto/blockdump/v2/rpc"
)

// IBlockFactory handles the creation of IBlock instances from different sources.
// It abstracts away the details of block construction and data fetching,
// providing a clean interface for creating blocks.
type IBlockFactory interface {
	// DecodeBlock constructs an IBlock from BSON-encoded data
	DecodeBlock(blockData io.Reader) (IBlock, error)

	// LookupBlockFromHash combines data fetching with block creation
	LookupBlockFromHash(ctx context.Context, hash string, rpc rpc.IBasicBChainRPC) (IBlock, error)
}

// BlockFactory provides a concrete implementation of IBlockFactory
// that leverages the Block type's built-in BSON serialization capabilities
type BlockFactory struct{}

// NewBlockFactory returns a new IBlockFactory instance.
// We return the interface rather than the concrete type to maintain abstraction
// and allow for different implementations in the future.
func NewBlockFactory() IBlockFactory {
	return &BlockFactory{}
}

// DecodeBlock takes BSON-encoded block data and produces an IBlock instance.
// It relies on the Block type's Decode method to handle deserialization.
func (self *BlockFactory) DecodeBlock(blockData io.Reader) (IBlock, error) {
	block := &Block{}
	result, err := block.Decode(blockData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block data: %w", err)
	}
	return result, nil
}

// LookupBlockFromHash fetches block data from the blockchain via RPC
// and creates an IBlock instance from it. This method combines network
// retrieval with block creation in a single convenient operation.
func (self *BlockFactory) LookupBlockFromHash(ctx context.Context, hash string, rpc rpc.IBasicBChainRPC) (IBlock, error) {
	// Define result types to hold our parallel operation results
	type blockReaderResult struct {
		reader io.Reader
		err    error
	}
	type blockJSONResult struct {
		json map[string]interface{}
		err  error
	}

	// Create channels to receive results
	readerChan := make(chan blockReaderResult, 1)
	jsonChan := make(chan blockJSONResult, 1)

	// Launch goroutine to fetch block reader
	go func() {
		reader, err := rpc.GetBlockReader(ctx, hash)
		readerChan <- blockReaderResult{reader: reader, err: err}
	}()

	// Launch goroutine to fetch block JSON
	go func() {
		blockJSON, err := rpc.GetBlockJSON(ctx, hash)
		jsonChan <- blockJSONResult{json: blockJSON, err: err}
	}()

	// Collect results from both operations
	readerResult := <-readerChan
	jsonResult := <-jsonChan

	// Check for errors from either operation
	if readerResult.err != nil {
		return nil, fmt.Errorf("failed to get block data for hash %s: %w", hash, readerResult.err)
	}
	if jsonResult.err != nil {
		return nil, fmt.Errorf("failed to get block JSON for hash %s: %w", hash, jsonResult.err)
	}

	// Read all the binary data into a byte slice
	blockBytes, err := io.ReadAll(readerResult.reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read block data for hash %s: %w", hash, err)
	}

	// Extract required fields from the JSON response
	blockID, ok := jsonResult.json["height"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid or missing block height for hash %s", hash)
	}

	// Get the parent block hash
	parentHash, ok := jsonResult.json["previousblockhash"].(string)
	if !ok {
		// Handle genesis block case where there is no parent
		if blockID == 0 {
			parentHash = ""
		} else {
			return nil, fmt.Errorf("invalid or missing previous block hash for hash %s", hash)
		}
	}

	// Create and return the Block instance
	return &Block{
		id:          int(blockID),
		hash:        hash,
		bytes:       blockBytes,
		parent_hash: parentHash,
	}, nil
}

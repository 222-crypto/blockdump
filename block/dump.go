package block

import (
	"context"
	"crypto/rand"
	"fmt"
	"iter"
	"math/big"

	"github.com/222-crypto/blockdump/v2/block/constants"
	"github.com/222-crypto/blockdump/v2/encoding"
	"github.com/222-crypto/blockdump/v2/rpc"
)

type IBlockDump interface {
	GenesisBlock(ctx context.Context) (IBlock, error)
	AllBlocks(ctx context.Context) (encoding.SeqEncoder[IBlock], error)
	SpecificBlock(ctx context.Context, blockid int) (IBlock, error)
	BlockRange(ctx context.Context, start, end int) (encoding.SeqEncoder[IBlock], error)
	RandomBlock(ctx context.Context) (IBlock, error)
	RandomBlockSample(ctx context.Context, samplesize int) (encoding.SeqEncoder[IBlock], error)
}

// BlockDump implements IBlockDump interface using blockchain RPC and iterator capabilities.
// It provides methods to access blocks in various ways: individually, in ranges,
// or randomly sampled.
type BlockDump struct {
	rpc           rpc.IBasicBChainRPC
	iterator      IBlockIterator
	block_factory IBlockFactory
}

// NewBlockDump creates a new BlockDump instance with the required dependencies.
func NewBlockDump(r rpc.IBasicBChainRPC, iterator IBlockIterator) (*BlockDump, error) {
	if r == nil {
		return nil, fmt.Errorf("rpc must not be nil")
	}
	if iterator == nil {
		return nil, fmt.Errorf("iterator must not be nil")
	}
	return &BlockDump{
		rpc:           r,
		iterator:      iterator,
		block_factory: NewBlockFactory(),
	}, nil
}

// GenesisBlock retrieves the first block in the blockchain.
// This implementation assumes GENESIS_BLOCK_ID is 0, as defined in the constants.
func (self *BlockDump) GenesisBlock(ctx context.Context) (IBlock, error) {
	// Get the hash of the genesis block
	hash, err := self.rpc.GetBlockhash(ctx, constants.GENESIS_BLOCK_ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get genesis block hash: %w", err)
	}

	// Use the factory to create the block from its hash
	block, err := self.block_factory.LookupBlockFromHash(ctx, hash, self.rpc)
	if err != nil {
		return nil, fmt.Errorf("failed to create genesis block: %w", err)
	}

	return block, nil
}

// AllBlocks returns a sequence containing every block in the blockchain.
// The blocks are returned in ascending order from genesis to the latest block.
func (self *BlockDump) AllBlocks(ctx context.Context) (encoding.SeqEncoder[IBlock], error) {
	// Get the current block count
	count, err := self.rpc.GetBlockCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block count: %w", err)
	}

	// Get all blocks from genesis (0) to the latest block
	return self.BlockRange(ctx, constants.GENESIS_BLOCK_ID, count)
}

// SpecificBlock retrieves a single block by its ID.
// It validates the block ID before attempting to retrieve it.
func (self *BlockDump) SpecificBlock(ctx context.Context, blockid int) (IBlock, error) {
	// Validate the block ID
	valid, err := self.rpc.IsValidBlockID(ctx, blockid)
	if err != nil {
		return nil, fmt.Errorf("failed to validate block ID %d: %w", blockid, err)
	}
	if !valid {
		return nil, fmt.Errorf("invalid block ID: %d", blockid)
	}

	// Get the block hash
	hash, err := self.rpc.GetBlockhash(ctx, blockid)
	if err != nil {
		return nil, fmt.Errorf("failed to get hash for block %d: %w", blockid, err)
	}

	// Create the block using the factory
	block, err := self.block_factory.LookupBlockFromHash(ctx, hash, self.rpc)
	if err != nil {
		return nil, fmt.Errorf("failed to create block %d: %w", blockid, err)
	}

	return block, nil
}

// BlockRange returns a sequence of blocks within the specified range [start, end].
// It uses the IBlockIterator for efficient block retrieval and implements streaming
// block fetching rather than loading all blocks into memory at once.
func (self *BlockDump) BlockRange(ctx context.Context, start, end int) (encoding.SeqEncoder[IBlock], error) {
	// Validate the range
	if start < 0 {
		return nil, fmt.Errorf("start block ID cannot be negative: %d", start)
	}
	if end < start {
		return nil, fmt.Errorf("end block ID (%d) must be greater than or equal to start block ID (%d)", end, start)
	}

	// Get the current block count to validate the range
	count, err := self.rpc.GetBlockCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block count: %w", err)
	}

	// Ensure the range doesn't exceed the blockchain height
	if start > count {
		return nil, fmt.Errorf("start block ID (%d) exceeds blockchain height (%d)", start, count)
	}
	if end > count {
		end = count // Adjust end to match blockchain height
	}

	// Create a sequence that will stream blocks as they're fetched
	blockSeq := iter.Seq2[IBlock, error](func(yield func(IBlock, error) bool) {
		// Process each hash in the sequence
		for result, err := range self.iterator.GetBlocksByRange(ctx, start, end) {
			// Check for hash retrieval errors
			if err != nil {
				if !yield(nil, fmt.Errorf("failed to get block hash: %w", err)) {
					return
				}
				continue
			}

			// Create block from hash using the factory
			block, err := self.block_factory.LookupBlockFromHash(ctx, result, self.rpc)
			if err != nil {
				if !yield(nil, fmt.Errorf("failed to create block from hash %s: %w", result, err)) {
					return
				}
				continue
			}

			// Yield the block
			if !yield(block, nil) {
				return
			}
		}
	})

	// Create and return a new block sequence encoder
	return NewBlockSequenceEncoding(blockSeq), nil
}

// RandomBlock returns a single randomly selected block from the blockchain.
func (self *BlockDump) RandomBlock(ctx context.Context) (IBlock, error) {
	// Get the current block count
	count, err := self.rpc.GetBlockCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block count: %w", err)
	}

	// Generate a random block ID
	n, err := rand.Int(rand.Reader, big.NewInt(int64(count)))
	if err != nil {
		return nil, fmt.Errorf("failed to generate random number: %w", err)
	}
	blockID := int(n.Int64())

	// Get the specific block
	return self.SpecificBlock(ctx, blockID)
}

// RandomBlockSample returns a sequence of randomly selected unique blocks.
// The sample size is capped at the total number of blocks in the blockchain.
// It includes a safety limit for skipping problematic blocks to prevent infinite loops.
func (self *BlockDump) RandomBlockSample(ctx context.Context, samplesize int) (encoding.SeqEncoder[IBlock], error) {
	if samplesize <= 0 {
		return nil, fmt.Errorf("sample size must be positive: %d", samplesize)
	}

	// Get the current block count
	count, err := self.rpc.GetBlockCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block count: %w", err)
	}

	blockSeq := iter.Seq2[IBlock, error](func(yield func(IBlock, error) bool) {
		// Cap the sample size at the total number of blocks
		if samplesize > count {
			samplesize = count
		}

		// Generate random block IDs with error limit
		seen := make(map[int]bool)
		n_blocks_retrieved := 0
		const maxErrors = 100 // Limit the number of problematic blocks we'll skip

		errorCount := 0
		for n_blocks_retrieved < samplesize && errorCount < maxErrors {
			// Generate random block ID
			n, err := rand.Int(rand.Reader, big.NewInt(int64(count)))
			if err != nil {
				yield(nil, fmt.Errorf("failed to generate random number: %w", err))
				return
			}
			blockID := int(n.Int64())

			// Skip if we've seen this block
			if seen[blockID] {
				continue
			}
			seen[blockID] = true

			// Get the specific block
			block, err := self.SpecificBlock(ctx, blockID)
			if err != nil {
				errorCount++
				continue
			}

			yield(block, nil)
			n_blocks_retrieved++
		}

		// Check if we hit the error limit
		if errorCount >= maxErrors {
			yield(nil, fmt.Errorf("exceeded maximum number of problematic blocks (%d) while sampling", maxErrors))
			return
		}
	})

	return NewBlockSequenceEncoding(blockSeq), nil
}

package block

import (
	"context"
	"fmt"
	"iter"

	"github.com/222-crypto/blockdump/v2/rpc"
)

type IBlockIterator interface {
	// For efficient forward/backward iteration
	GetBlocksByRange(ctx context.Context, start, end int) iter.Seq2[string, error] // returns sequence of block hashes
}

// BlockIterator handles efficient iteration over blockchain blocks by managing
// block hash retrieval in ranges. It uses the RPC client for actual data fetching.
type BlockIterator struct {
	rpc rpc.IBasicBChainRPC
}

// NewBlockIterator creates a new BlockIterator instance that will use the provided
// RPC client for block hash retrieval operations.
func NewBlockIterator(r rpc.IBasicBChainRPC) *BlockIterator {
	return &BlockIterator{
		rpc: r,
	}
}

// GetBlocksByRange implements IBlockIterator by returning a sequence of block hashes
// for the specified range. It handles validation and error checking while retrieving
// hashes through the RPC client.
func (bi *BlockIterator) GetBlocksByRange(ctx context.Context, start, end int) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		// Validate range before starting iteration
		if start < 0 || end < start {
			yield("", fmt.Errorf("invalid block range: start=%d, end=%d", start, end))
			return
		}

		// Iterate through the range, fetching block hashes
		for blockID := start; blockID <= end; blockID++ {
			hash, err := bi.rpc.GetBlockhash(ctx, blockID)
			if !yield(hash, err) {
				return // Stop if yield returns false
			}
		}
	}
}

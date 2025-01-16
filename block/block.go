// Package block provides core functionality for handling blockchain blocks in the blockdump system.
//
// # Overview
//
// The block package implements the fundamental block operations needed for blockchain
// interaction. It provides interfaces and implementations for block creation,
// manipulation, encoding/decoding, and iteration. This package serves as the foundation
// for all block-related operations in the blockdump system.
//
// # Key Components
//
// The package consists of several key components:
//   - Block: The primary implementation of IBlock, representing a blockchain block
//   - BlockDump: Implementation of IBlockDump for high-level block operations
//   - BlockFactory: Handles block creation and instantiation
//   - BlockIterator: Manages efficient block iteration
//   - BlockSequence: Handles sequences of blocks with proper error handling
//
// Core Interfaces
//
//   - IBlock: Defines the core block functionality
//     type IBlock interface {
//     encoding.Encoder
//     encoding.Decoder[IBlock]
//     ID() int
//     Hash() string
//     Bytes() []byte
//     Parent(context.Context, IBlockLookup) (IBlock, error)
//     ParentHash() string
//     }
//
//   - IBlockDump: Provides high-level block operations
//     type IBlockDump interface {
//     GenesisBlock(ctx context.Context) (IBlock, error)
//     AllBlocks(ctx context.Context) (encoding.SeqEncoder[IBlock], error)
//     SpecificBlock(ctx context.Context, blockid int) (IBlock, error)
//     BlockRange(ctx context.Context, start, end int) (encoding.SeqEncoder[IBlock], error)
//     RandomBlock(ctx context.Context) (IBlock, error)
//     RandomBlockSample(ctx context.Context, samplesize int) (encoding.SeqEncoder[IBlock], error)
//     }
//
// # Usage
//
// Basic block retrieval:
//
//	// Create a new BlockDump instance
//	rpcClient := // Initialize RPC client
//	iterator := block.NewBlockIterator(rpcClient)
//	dumper, err := block.NewBlockDump(rpcClient, iterator)
//	if err != nil {
//		return err
//	}
//
//	// Retrieve a specific block
//	block, err := dumper.SpecificBlock(ctx, blockID)
//	if err != nil {
//		return err
//	}
//
// Iterating over blocks:
//
//	// Get a range of blocks
//	blocks, err := dumper.BlockRange(ctx, startBlock, endBlock)
//	if err != nil {
//		return err
//	}
//
//	// Process the blocks
//	blocks.Seq()(func(block IBlock) bool {
//		// Process each block
//		return true // continue iteration
//	})
//
// # Error Handling
//
// This package implements comprehensive error handling:
//   - All public methods return errors with context
//   - Asynchronous operations use error channels via error_handling.ErrorChanneler
//   - Network operations implement retry logic with exponential backoff
//   - Invalid inputs are checked and return appropriate errors
//
// Error Channel Example:
//
//	// Create a block sequence
//	seq := NewBlockSequenceEncoding(blockSeq)
//
//	// Monitor for errors during processing
//	go func() {
//		for err := range seq.RErrorChannel() {
//			// Handle async errors
//		}
//	}()
//
// # Thread Safety
//
// The block package implements thread-safe operations:
//   - All public methods are safe for concurrent use
//   - Internal state is protected by mutexes where necessary
//   - Channel operations are used for safe concurrent communication
//   - Immutable data structures are used where possible
//
// # Performance Considerations
//
// The package implements several optimizations:
//   - Efficient block iteration using iter.Seq2
//   - Streaming block processing to minimize memory usage
//   - Concurrent operations where beneficial
//   - Proper resource cleanup using defer statements
//
// # Implementation Notes
//
// Key design decisions:
//   - Blocks are immutable once created
//   - BSON is used for block serialization
//   - Error channels provide async error handling
//   - Interface-based design enables custom implementations
//   - Context support for operation cancellation
//
// # See Also
//
// Related packages:
//   - encoding: Provides serialization interfaces
//   - rpc: Implements blockchain RPC communication
//   - error_handling: Defines error handling patterns
package block

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/222-crypto/blockdump/v2/encoding"
	"github.com/222-crypto/blockdump/v2/error_handling"
)

type IBlock interface {
	encoding.Encoder
	encoding.Decoder[IBlock]
	ID() int
	Hash() string
	Bytes() []byte
	Parent(context.Context, IBlockLookup) (IBlock, error)
	ParentHash() string
}

type Block struct {
	id          int
	hash        string
	bytes       []byte
	parent_hash string
	ec          error_handling.ErrorChanneler
}

func NewBlock(id int, hash string, bytes []byte, parentHash string) IBlock {
	return &Block{
		id:          id,
		hash:        hash,
		bytes:       bytes,
		parent_hash: parentHash,
	}
}

func (self *Block) ID() int {
	return self.id
}

func (self *Block) Hash() string {
	return self.hash
}

func (self *Block) Bytes() []byte {
	return self.bytes
}

func (self *Block) RErrorChannel() <-chan error {
	return self.ec.RErrorChannel()
}

func (self *Block) Encode() io.Reader {
	pipe_reader, pipe_writer := io.Pipe()
	error_channel := self.ec.ErrorChannel()

	go func() {
		defer pipe_writer.Close()
		defer close(error_channel)

		encode_data := map[string]interface{}{
			"i": self.id,
			"h": self.hash,
			"b": self.bytes,
			"p": self.parent_hash,
		}

		// Encode block data
		data, err := bson.Marshal(encode_data)
		if err != nil {
			error_handling.PipeWriterPanic(true, pipe_writer, error_channel, err)
			return
		}

		// Write size prefix
		size := uint32(len(self.bytes))
		if err := binary.Write(pipe_writer, binary.LittleEndian, size); err != nil {
			error_handling.PipeWriterPanic(true, pipe_writer, error_channel,
				fmt.Errorf("failed to write block size: %w", err))
			return
		}

		// Write block data
		_, err = pipe_writer.Write(data)
		if err != nil {
			error_handling.PipeWriterPanic(true, pipe_writer, error_channel, err)
			return
		}
	}()

	return pipe_reader
}

func (self *Block) Decode(r io.Reader) (IBlock, error) {
	// Read size prefix (uint32)
	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return nil, fmt.Errorf("failed to read block size: %w", err)
	}

	// Check if the size is valid
	if size == 0 {
		return nil, fmt.Errorf("invalid block size: %d", size)
	}

	// Read BSON data
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("failed to read block data: %w", err)
	}

	// Unmarshal the BSON data
	var decoded struct {
		ID         int    `bson:"i"`
		Hash       string `bson:"h"`
		Bytes      []byte `bson:"b"`
		ParentHash string `bson:"p"`
	}

	if err := bson.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block data: %w", err)
	}

	// Create and return a new Block instance
	return NewBlock(
		decoded.ID,
		decoded.Hash,
		decoded.Bytes,
		decoded.ParentHash,
	), nil
}

func (self *Block) Parent(ctx context.Context, lookup IBlockLookup) (IBlock, error) {
	return lookup.GetBlockByHash(ctx, self.parent_hash)
}

func (self *Block) ParentHash() string {
	return self.parent_hash
}

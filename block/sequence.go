package block

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"iter"
	"os"

	"github.com/222-crypto/blockdump/v2/encoding"
	"github.com/222-crypto/blockdump/v2/error_handling"
)

// BlockSequence implements encoding.SeqEncoder[IBlock] to provide a way to encode and decode
// sequences of blocks. It handles both the sequential nature of blocks and their
// encoding/decoding operations while maintaining consistent error handling patterns.
//
// For iterating over blocks, use the Seq2() method which provides explicit error handling:
//
//	for block, err := range sequence.Seq2() {
//	    if err != nil {
//	        // Handle error
//	        continue
//	    }
//	    // Process block
//	}
//
// The error channel (RErrorChannel) is primarily used during encoding operations
// when streaming blocks to an output. Most users won't need to interact with it
// directly as WriteEncoder handles this internally.
type BlockSequence struct {
	seq iter.Seq2[IBlock, error]
	ec  error_handling.ErrorChanneler
}

// NewBlockSequenceEncoding creates a new BlockSequence from an iterator sequence.
// The error channel is created lazily when needed, following the pattern used
// in the Block implementation.
func NewBlockSequenceEncoding(seq iter.Seq2[IBlock, error]) encoding.SeqEncoder[IBlock] {
	return &BlockSequence{
		seq: seq,
	}
}

// RErrorChannel returns a read-only channel for error notifications,
// ensuring encapsulation of the error channel.
func (self *BlockSequence) RErrorChannel() <-chan error {
	return self.ec.RErrorChannel()
}

// Seq2 returns the underlying block sequence iterator with error propagation.
// This method satisfies the SeqEncoder interface requirement and provides direct
// access to the error-aware sequence.
func (self *BlockSequence) Seq2() iter.Seq2[IBlock, error] {
	return self.seq
}

// Seq returns the underlying block sequence iterator, propagating errors
// through the error channel using SendError.
func (self *BlockSequence) Seq() iter.Seq[IBlock] {
	error_channel := self.ec.ErrorChannel()
	return func(yield func(IBlock) bool) {
		self.seq(func(block IBlock, err error) bool {
			if err != nil {
				// Use SendError for consistent error handling
				error_handling.SendError(error_channel, func(err error) {
					panic(fmt.Errorf("BlockSequence.Seq could not send error to channel: %w", err))
				}, err)
				return false
			}
			return yield(block)
		})
	}
}

// Encode implements the Encoder interface by converting the block sequence
// into a BSON-encoded stream of blocks. It uses PipeWriterPanic for consistent
// error handling during the encoding process.
func (self *BlockSequence) Encode() io.Reader {
	pipe_reader, pipe_writer := io.Pipe()
	error_channel := self.ec.ErrorChannel()

	go func() {
		defer pipe_writer.Close()
		defer close(error_channel)

		self.seq(func(block IBlock, err error) bool {
			if err != nil {
				error_handling.PipeWriterPanic(true, pipe_writer, error_channel,
					fmt.Errorf("sequence error: %w", err))
				return false
			}

			if block == nil {
				error_handling.PipeWriterPanic(true, pipe_writer, error_channel,
					fmt.Errorf("nil block in sequence"))
				return false
			}

			// Get block data
			reader := block.Encode()

			// Copy block data and flush after each block
			_, err = io.Copy(pipe_writer, reader)
			if err != nil {
				error_handling.PipeWriterPanic(true, pipe_writer, error_channel,
					fmt.Errorf("failed to write block data: %w", err))
				return false
			}

			// Check for block encoding errors
			select {
			case err := <-block.RErrorChannel():
				if err != nil {
					error_handling.PipeWriterPanic(true, pipe_writer, error_channel,
						fmt.Errorf("block encoding error: %w", err))
					return false
				}
			default:
			}

			return true
		})
	}()

	return pipe_reader
}

// Decode implements the Decoder interface by reading a stream of BSON-encoded blocks
// and reconstructing the block sequence. It maintains consistent error handling patterns
// with the rest of the codebase.
// BlockSequence.Decode implements the deserialization of a stream of BSON-encoded blocks
func (self *BlockSequence) Decode(r io.Reader) (encoding.SeqEncoder[IBlock], error) {
	// Create a buffered reader for more efficient reading
	br := bufio.NewReader(r)

	// Create a sequence that reads and decodes blocks from the stream
	blockSeq := iter.Seq2[IBlock, error](func(yield func(IBlock, error) bool) {
		for {
			// Create a new Block instance to decode into
			block := &Block{}

			// Attempt to decode the next block
			decoded, err := block.Decode(br)
			if err != nil {
				if err == io.EOF {
					// End of stream reached
					return
				}
				// Propagate any other errors through the sequence
				if !yield(nil, fmt.Errorf("failed to decode block: %w", err)) {
					return
				}
				continue
			}

			// Yield the decoded block
			if !yield(decoded, nil) {
				return
			}
		}
	})

	// Create and return a new BlockSequence with the decoded blocks
	return NewBlockSequenceEncoding(blockSeq), nil
}

// BlockSequenceFromReader creates a new BlockSequence by decoding BSON-encoded blocks
// from an io.Reader source. This function serves as the foundation for creating block
// sequences, with other functions like BlockSequenceFromFile building upon it.
//
// The function accepts any io.Reader that provides BSON-encoded block data matching
// the Block structure's expected encoding. Processing is done sequentially, allowing
// for efficient streaming of large datasets with memory usage that scales with
// individual block size rather than total sequence size.
//
// Error handling preserves the full context through error wrapping, allowing for
// effective debugging while maintaining the original error information. The function
// returns immediately on decode failures to prevent corrupt data processing.
//
// The returned sequence encoder is not thread-safe - concurrent access should be
// synchronized by the caller if needed.
//
// Parameters:
//   - reader: io.Reader - Any reader providing BSON-encoded block data
//
// Returns:
//   - encoding.SeqEncoder[IBlock] - A sequence encoder for accessing the decoded blocks
//   - error - Any error encountered during the decoding process
//
// Usage Example:
//
//	sequence, err := BlockSequenceFromReader(networkReader)
//	if err != nil {
//	    log.Fatalf("Failed to create sequence: %v", err)
//	}
//	for block, err := range sequence.Seq2() {
//	    if err != nil {
//	        log.Printf("Block error: %v", err)
//	        continue
//	    }
//	    // Process block
//	}
func BlockSequenceFromReader(reader io.Reader) (encoding.SeqEncoder[IBlock], error) {
	// Create a block sequence
	blockSeq := &BlockSequence{}

	// Decode the block sequence
	sequence, err := blockSeq.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("[BlockSequenceFromReader(%v)] failed to decode block sequence: %w", reader, err)
	}

	return sequence, nil
}

// BlockSequenceFromFile creates a new BlockSequence by reading and decoding blocks from
// a file containing BSON-encoded block data. This implementation loads the entire file
// into memory before decoding, trading increased memory usage for reduced I/O operations.
//
// The function uses bytes.Reader for efficient in-memory reading after loading the file.
// Memory usage will be approximately the size of the input file, making this approach
// unsuitable for files larger than available RAM.
//
// Parameters:
//   - filename: string - Path to the file containing BSON-encoded block data
//
// Returns:
//   - encoding.SeqEncoder[IBlock] - A sequence encoder for accessing the decoded blocks
//   - error - Any error encountered during file operations or decoding
//
// Usage Example:
//
//	sequence, err := BlockSequenceFromFile("blocks.dat")
//	if err != nil {
//	    log.Fatalf("Failed to load blocks: %v", err)
//	}
//	for block, err := range sequence.Seq2() {
//	    if err != nil {
//	        log.Printf("Block error: %v", err)
//	        continue
//	    }
//	    // Process block
//	}
func BlockSequenceFromFile(filename string) (encoding.SeqEncoder[IBlock], error) {
	// Open the block data file
	blockFile, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("[BlockSequenceFromFile(%s)] failed to open block file: %w", filename, err)
	}
	defer blockFile.Close()

	// Read the entire file into a buffer
	fileContents, err := io.ReadAll(blockFile)
	if err != nil {
		return nil, fmt.Errorf("[BlockSequenceFromFile(%s)] failed to read file contents: %w", filename, err)
	}

	// Create and decode the block sequence using our buffered data
	sequence, err := BlockSequenceFromReader(bytes.NewReader(fileContents))
	if err != nil {
		return nil, fmt.Errorf("[BlockSequenceFromFile(%s)] failed to decode block sequence: %w", filename, err)
	}

	return sequence, nil
}

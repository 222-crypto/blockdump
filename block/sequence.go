package block

import (
	"bufio"
	"fmt"
	"io"
	"iter"

	"github.com/222-crypto/blockdump/v2/encoding"
	"github.com/222-crypto/blockdump/v2/error_handling"
)

// BlockSequence implements encoding.SeqEncoder[IBlock] to provide a way to encode and decode
// sequences of blocks. It handles both the sequential nature of blocks and their
// encoding/decoding operations while maintaining consistent error handling patterns.
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

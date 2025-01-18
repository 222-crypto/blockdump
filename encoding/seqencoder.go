package encoding

import (
	"iter"

	"github.com/222-crypto/blockdump/v2/error_handling"
)

// SeqEncoder represents a sequence of encodable values that can be iterated or encoded as a stream.
type SeqEncoder[T any] interface {
	Encoder
	error_handling.IReceiveErrorChanneler // shared by Seq and Encoder
	Decoder[SeqEncoder[T]]

	// Seq returns an iterator that sends errors through the error channel.
	// This method exists primarily for compatibility with iter.Seq.
	// For new code, prefer using Seq2 which provides explicit error handling.
	Seq() iter.Seq[T]

	// Seq2 returns an iterator that explicitly includes errors in its return values.
	// This is the recommended method for iterating over sequences as it provides
	// immediate error handling without requiring error channel monitoring.
	Seq2() iter.Seq2[T, error]
}

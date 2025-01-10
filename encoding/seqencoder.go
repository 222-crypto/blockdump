package encoding

import (
	"iter"

	"github.com/222-crypto/blockdump/v2/error_handling"
)

type SeqEncoder[T any] interface {
	Encoder
	error_handling.IReceiveErrorChanneler // shared by Seq and Encoder
	Decoder[SeqEncoder[T]]
	Seq() iter.Seq[T]
}

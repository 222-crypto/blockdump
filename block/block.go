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

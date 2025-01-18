// This program parses block data files created by the blockdump system, providing detailed
// information about each block's structure and content. It reads a binary file containing
// a sequence of blockchain blocks that were previously saved using the blockdump tool's
// binary output format.
//
// File Format:
// Each block in the file consists of two main parts:
// 1. A uint32 size prefix indicating the length of the block's raw bytes field
// 2. A BSON document containing the block's metadata and content:
//   - i: Block ID (height in the blockchain)
//   - h: Block hash (unique identifier)
//   - b: Raw block bytes
//   - p: Parent block hash (forms the chain structure)
//
// The program reads through the file sequentially, processing each block and displaying:
// - The size of the block's bytes field
// - The size of the BSON document
// - Decoded block information including:
//   - Block ID
//   - Block hash
//   - Parent block hash
//   - Size of the block's raw data
//
// At the end, it provides summary statistics including:
// - Total number of blocks processed
// - Total size of all block data combined
//
// Error Handling:
// The program implements robust error checking at each step:
// - File opening and reading
// - Binary size field parsing
// - BSON document structure validation
// - Data integrity verification
//
// Usage:
//
//	./blockparser saved_blocks.dat
//
// The program expects the input file to have been created using blockdump's binary
// output format (-b flag). It will process the file until either reaching the end
// or encountering an error, at which point it will display the error and the
// statistics for the blocks successfully processed up to that point.
package main

import (
	"encoding/binary"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"io"
	"os"
)

func main() {
	// Verify correct usage
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <block_file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Analyzes a binary block file created by blockdump -b\n")
		os.Exit(1)
	}

	// Open the block file
	blockFile, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open block file: %v\n", err)
		os.Exit(1)
	}
	defer blockFile.Close()

	// Track statistics promised in the header
	var totalBlocks int
	var totalSize int64

	// Process blocks until EOF or error
	for {
		// Step 1: Read the bytes field size prefix (uint32)
		var bytesSize uint32
		err := binary.Read(blockFile, binary.LittleEndian, &bytesSize)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading bytes size: %v\n", err)
			break
		}
		fmt.Printf("Block %d: bytes field size=%d\n", totalBlocks+1, bytesSize)

		// Step 2: Read and validate the BSON document
		// BSON documents start with a 4-byte length prefix
		var bsonSize uint32
		if err := binary.Read(blockFile, binary.LittleEndian, &bsonSize); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading BSON size: %v\n", err)
			break
		}
		if bsonSize <= 4 || bsonSize > 1e8 { // Basic sanity check on BSON size
			fmt.Fprintf(os.Stderr, "Invalid BSON document size: %d\n", bsonSize)
			break
		}
		fmt.Printf("  BSON document size: %d\n", bsonSize)

		// Read the complete BSON document
		bsonData := make([]byte, bsonSize)
		binary.LittleEndian.PutUint32(bsonData, bsonSize) // Restore length prefix
		if _, err := io.ReadFull(blockFile, bsonData[4:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading BSON data: %v\n", err)
			break
		}

		// Step 3: Decode and validate the block structure
		var decoded struct {
			ID         int    `bson:"i"` // Block ID (height)
			Hash       string `bson:"h"` // Block hash
			Bytes      []byte `bson:"b"` // Raw block data
			ParentHash string `bson:"p"` // Parent block hash
		}
		if err := bson.Unmarshal(bsonData, &decoded); err != nil {
			fmt.Fprintf(os.Stderr, "Error decoding BSON: %v\n", err)
			break
		}

		// Validate block data integrity
		if len(decoded.Bytes) != int(bytesSize) {
			fmt.Fprintf(os.Stderr, "Block data size mismatch: expected %d, got %d\n",
				bytesSize, len(decoded.Bytes))
			break
		}

		// Update statistics
		totalBlocks++
		totalSize += int64(len(decoded.Bytes))

		// Display block information as promised in the header
		fmt.Printf("  Block info:\n"+
			"    ID: %d\n"+
			"    Hash: %s\n"+
			"    Parent: %s\n"+
			"    Data size: %d bytes\n\n",
			decoded.ID, decoded.Hash, decoded.ParentHash, len(decoded.Bytes))
	}

	// Display summary statistics
	fmt.Printf("Successfully processed %d blocks (total data: %d bytes)\n",
		totalBlocks, totalSize)
}

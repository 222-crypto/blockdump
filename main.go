package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"iter"
	"math"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

/* blockdump
blockdump is a command-line utility for dumping blocks via gen1 Blockchain RPC.

Supported cryptocurrencies:
 * CLAM (Clamcoin)

Usage:
blockdump [connection-options] -[b] -[f <file>] <subcommand> [<args>]

Connection Options:
  -rpcconnect=<ip>
       Send commands to node running on <ip> (default: 127.0.0.1)

  -rpcport=<port>
       Connect to JSON-RPC on <port> (default: 30174 or testnet: 35715)

  -rpcuser=<user>
       Username for JSON-RPC connections (default: "${BLOCKDUMP_RPC_USER}")

  -rpcpassword=<pw>
       Password for JSON-RPC connections (default: "${BLOCKDUMP_RPC_PASSWORD}")

  -rpcclienttimeout=<n>
       Timeout during HTTP requests (default: 900)

  -rpcretrylimit=<n>
       Retry limit for JSON-RPC requests (default: 3)

  -rpcretrybackoffmax=<n>
       Maximum backoff time for JSON-RPC retries (default: 300)

  -rpcretrybackoffmin=<n>
       Minimum backoff time for JSON-RPC retries (default: 1)

Flags:
-b binary output (default: false)
-f <file> output to file (default: stdout)

Commands:
                  genesisblock - Retrieve the first block in the blockchain
                     allblocks - Retrieve all blocks in the blockchain
            specificblock <id> - Retrieve a specific block by ID
      blockrange <start> <end> - Retrieve a range of blocks by ID
                   randomblock - Retrieve a random block from the blockchain
randomblocksample <samplesize> - Retrieve a random sample of blocks
*/

type IBlockDump interface {
	GenesisBlock(ctx context.Context) (IBlock, error)
	AllBlocks(ctx context.Context) (SeqEncoder[IBlock], error)
	SpecificBlock(ctx context.Context, blockid int) (IBlock, error)
	BlockRange(ctx context.Context, start, end int) (SeqEncoder[IBlock], error)
	RandomBlock(ctx context.Context) (IBlock, error)
	RandomBlockSample(ctx context.Context, samplesize int) (SeqEncoder[IBlock], error)
}

func ChanSeq[T any](seq iter.Seq[T]) <-chan T {
	ch := make(chan T)
	go func() {
		// cannot range over seq (variable of type iter.Seq[T])
		seq(func(item T) bool {
			ch <- item
			return true
		})

		close(ch)
	}()
	return ch
}

type RPCConfig struct {
	Connect         string
	Port            int
	User            string
	Password        string
	ClientTimeout   time.Duration
	RetryLimit      int
	RetryBackoffMax time.Duration
	RetryBackoffMin time.Duration
}

type IBasicBChainRPC interface {
	// config ops
	BasicConfig() RPCConfig
	UpdateConfig(config func() RPCConfig) error // returns an error if the config is invalid

	// raw ops
	GetBlockReader(ctx context.Context, blockhash string) (io.Reader, error)            // raw block data
	GetBlockJSON(ctx context.Context, blockhash string) (map[string]interface{}, error) // max verbosity JSON

	// lookup ops
	GetBlockhash(ctx context.Context, blockid int) (string, error)
	GetBestBlockhash(ctx context.Context) (string, error)
	GetBlockCount(ctx context.Context) (int, error)

	// validation ops
	IsValidBlockID(ctx context.Context, id int) (bool, error)
}

// Command represents the subcommand to execute
type Command struct {
	Name string   // The name of the command (e.g., "genesisblock", "allblocks", etc.)
	Args []string // Any additional arguments for the command
}

// CommandLineConfig holds all command-line configuration options
type CommandLineConfig struct {
	*RPCConfig // Embedded RPC configuration

	// Output Settings
	BinaryOutput bool   // Whether to output in binary format (-b flag)
	OutputFile   string // File to write output to (-f flag), empty means stdout

	// Command to execute (parsed from remaining arguments)
	Command *Command
}

// DefaultCommandLineConfig returns a CommandLineConfig with default values set
func DefaultCommandLineConfig() *CommandLineConfig {
	return &CommandLineConfig{
		RPCConfig: &RPCConfig{
			Connect:         "127.0.0.1",
			Port:            30174, // Default mainnet port
			User:            os.Getenv("BLOCKDUMP_RPC_USER"),
			Password:        os.Getenv("BLOCKDUMP_RPC_PASSWORD"),
			ClientTimeout:   900 * time.Second,
			RetryLimit:      3,
			RetryBackoffMax: 300 * time.Second,
			RetryBackoffMin: 1 * time.Second,
		},
		BinaryOutput: false, // Default to text output
		Command:      nil,   // Must be specified by user
	}
}

// ParseConfig parses command-line arguments and returns a filled Config
func ParseConfig() (*CommandLineConfig, error) {
	config := DefaultCommandLineConfig()

	// Define flags
	flag.StringVar(&config.Connect, "rpcconnect", config.Connect,
		"Send commands to node running on <ip>")
	flag.IntVar(&config.Port, "rpcport", config.Port,
		"Connect to JSON-RPC on <port>")
	flag.StringVar(&config.User, "rpcuser", "${BLOCKDUMP_RPC_USER}",
		"Username for JSON-RPC connections")
	flag.StringVar(&config.Password, "rpcpassword", "${BLOCKDUMP_RPC_PASSWORD}",
		"Password for JSON-RPC connections")
	flag.DurationVar(&config.ClientTimeout, "rpcclienttimeout", config.ClientTimeout,
		"Timeout during HTTP requests (seconds)")
	flag.IntVar(&config.RetryLimit, "rpcretrylimit", config.RetryLimit,
		"Retry limit for JSON-RPC requests")
	flag.DurationVar(&config.RetryBackoffMax, "rpcretrybackoffmax", config.RetryBackoffMax,
		"Maximum backoff time for JSON-RPC retries (seconds)")
	flag.DurationVar(&config.RetryBackoffMin, "rpcretrybackoffmin", config.RetryBackoffMin,
		"Minimum backoff time for JSON-RPC retries (seconds)")

	// Output flags
	flag.BoolVar(&config.BinaryOutput, "b", config.BinaryOutput,
		"Use binary output format (default: false)")
	flag.StringVar(&config.OutputFile, "f", config.OutputFile,
		"Output file (default: stdout)")

	// Custom usage function to show our specific help
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: blockdump [connection-options] -[b] -[f <file>] <subcommand> [<args>]\n\n")
		fmt.Fprintf(os.Stderr, "Supported cryptocurrencies:\n")
		fmt.Fprintf(os.Stderr, " * CLAM (Clamcoin)\n\n")
		fmt.Fprintf(os.Stderr, "Connection Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nCommands:\n")
		fmt.Fprintf(os.Stderr, "                  genesisblock - Retrieve the first block in the blockchain\n")
		fmt.Fprintf(os.Stderr, "                     allblocks - Retrieve all blocks in the blockchain\n")
		fmt.Fprintf(os.Stderr, "            specificblock <id> - Retrieve a specific block by ID\n")
		fmt.Fprintf(os.Stderr, "      blockrange <start> <end> - Retrieve a range of blocks by ID\n")
		fmt.Fprintf(os.Stderr, "                   randomblock - Retrieve a random block from the blockchain\n")
		fmt.Fprintf(os.Stderr, "randomblocksample <samplesize> - Retrieve a random sample of blocks\n")
	}

	// Parse flags
	flag.Parse()

	// Handle remaining arguments as command and its arguments
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		return nil, fmt.Errorf("no command specified")
	}

	// Parse the command
	cmd, err := parseCommand(args)
	if err != nil {
		return nil, err
	}
	config.Command = cmd

	// Validate the configuration
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

// parseCommand parses the command and its arguments from the remaining command-line args
func parseCommand(args []string) (*Command, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command specified")
	}

	cmd := &Command{
		Name: args[0],
		Args: args[1:],
	}

	// Validate command and its arguments
	switch cmd.Name {
	case "genesisblock", "allblocks", "randomblock":
		if len(cmd.Args) != 0 {
			return nil, fmt.Errorf("%s command takes no arguments", cmd.Name)
		}
	case "specificblock":
		if len(cmd.Args) != 1 {
			return nil, fmt.Errorf("specificblock command requires exactly one argument: <id>")
		}
	case "blockrange":
		if len(cmd.Args) != 2 {
			return nil, fmt.Errorf("blockrange command requires exactly two arguments: <start> <end>")
		}
	case "randomblocksample":
		if len(cmd.Args) != 1 {
			return nil, fmt.Errorf("randomblocksample command requires exactly one argument: <samplesize>")
		}
	default:
		return nil, fmt.Errorf("unknown command: %s", cmd.Name)
	}

	return cmd, nil
}

// validateConfig performs validation on the entire configuration
func validateConfig(config *CommandLineConfig) error {
	// Validate RPCConfig
	if config.Port <= 0 {
		return fmt.Errorf("invalid RPC port: %d", config.Port)
	}
	if config.ClientTimeout <= 0 {
		return fmt.Errorf("invalid RPC client timeout: %v", config.ClientTimeout)
	}
	if config.RetryLimit < 0 {
		return fmt.Errorf("invalid RPC retry limit: %d", config.RetryLimit)
	}
	if config.RetryBackoffMin <= 0 {
		return fmt.Errorf("invalid minimum retry backoff: %v", config.RetryBackoffMin)
	}
	if config.RetryBackoffMax < config.RetryBackoffMin {
		return fmt.Errorf("maximum retry backoff (%v) must be greater than minimum (%v)",
			config.RetryBackoffMax, config.RetryBackoffMin)
	}

	return nil
}

// String implements fmt.Stringer, returning configuration as a single-line JSON string,
// excluding sensitive information like usernames and passwords
func (self *CommandLineConfig) String() string {
	// Create a map of the configuration, omitting sensitive data
	config := map[string]interface{}{
		"rpc": map[string]interface{}{
			"connect":           self.Connect,
			"port":              self.Port,
			"client_timeout":    self.ClientTimeout,
			"retry_limit":       self.RetryLimit,
			"retry_backoff_max": self.RetryBackoffMax,
			"retry_backoff_min": self.RetryBackoffMin,
		},
		"binary_output": self.BinaryOutput,
		"output_file":   self.OutputFile,
	}

	if self.Command != nil {
		config["command"] = map[string]interface{}{
			"name": self.Command.Name,
			"args": self.Command.Args,
		}
	}

	// Marshal to JSON, using compact encoding
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Sprintf("!error creating config string: %w!", err)
	}

	return string(data)
}

func main() {
	ctx := context.Background()

	// Run the main function and handle any errors
	output, err := _main(ctx, os.Stdin)

	if err != nil {
		if len(os.Args) > 1 {
			panic(err)
		} else {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	if output == nil {
		panic(fmt.Errorf("nil output reader returned"))
	}

	// Copy the output to stdout
	if _, err := io.Copy(os.Stdout, output); err != nil {
		panic(fmt.Errorf("failed to write output: %v", err))
	}
}

// WriteEncoder [with optional hex encoding] processes the Encoder's output
// and writes it to the provided writer.
// This design allows for efficient streaming of data while maintaining backpressure
// and proper error handling.
//
// Parameters:
//   - w: The writer that will provide the data to be encoded
//   - do_binary: If true, writes raw binary output; if false, writes hex-encoded output
//   - encoder: The encoder that will process the data stream
//
// Returns:
//   - error: Any error encountered during the writing or encoding process
//
// WriteEncoder processes an Encoder's output and writes it, either as raw binary
// or hex-encoded, to the provided writer while maintaining proper error handling.
func WriteEncoder(w io.Writer, do_binary bool, encoder Encoder) error {
	// Get the encoded data reader from the encoder
	reader := encoder.Encode()
	if reader == nil {
		return fmt.Errorf("encoder returned nil reader")
	}

	// Create error handling channels
	doneCh := make(chan struct{})
	errorCh := make(chan error, 1)

	// Start a goroutine to monitor the encoder's error channel
	go func() {
		defer close(doneCh)
		// Check for encoding errors from the encoder
		if encErr := <-encoder.ErrorChannel(); encErr != nil {
			errorCh <- fmt.Errorf("encoder error: %w", encErr)
		}
	}()

	// If binary output is requested, copy directly to writer
	if do_binary {
		_, err := io.Copy(w, reader)
		if err != nil {
			return fmt.Errorf("failed to write binary data: %w", err)
		}
	} else {
		// For hex output, create a hex encoder that writes to the output
		hexWriter := hex.NewEncoder(w)
		_, err := io.Copy(hexWriter, reader)
		if err != nil {
			return fmt.Errorf("failed to write hex-encoded data: %w", err)
		}
	}

	// Wait for error monitoring to complete
	<-doneCh

	// Check if any errors were reported
	select {
	case err := <-errorCh:
		return err
	default:
		return nil
	}
}

// _main processes command line arguments and executes the requested blockchain operation.
// It handles both file output and streaming stdout cases, ensuring proper error handling
// and resource cleanup.
//
// Parameters:
//   - ctx: The context for operation cancellation and timeout
//   - _: An io.Reader parameter (currently unused)
//
// Returns:
//   - output: An io.Reader that provides access to the operation results
//   - err: Any error encountered during setup or initialization
func _main(ctx context.Context, _ io.Reader) (output io.Reader, err error) {
	// Parse command line configuration
	config, err := ParseConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set up RPC client
	rpcClient, err := setupRPCClient(config.RPCConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to setup RPC client: %v", err)
	}

	// Create block iterator using the RPC client
	blockIterator := NewBlockIterator(rpcClient)

	// Create BlockDump instance that will handle the command execution
	blockDump, err := NewBlockDump(rpcClient, blockIterator)
	if err != nil {
		return nil, fmt.Errorf("failed to create block dumper: %v", err)
	}

	// Create a pipe for streaming output. This enables real-time data flow
	// instead of buffering all data before returning it.
	reader, writer := io.Pipe()

	// Start a goroutine to handle the writing process asynchronously.
	// This allows us to return the reader immediately while writing continues in the background.
	go func() {
		// Track any errors that occur during writing
		var writeErr error

		// Ensure proper cleanup of the writer when the goroutine exits
		defer func() {
			if writeErr != nil {
				// If an error occurred, propagate it through the pipe
				writer.CloseWithError(writeErr)
			} else {
				// Clean closure of the writer if no errors occurred
				writer.Close()
			}
		}()

		// Select the appropriate output destination based on configuration
		var outputWriter io.Writer
		if config.OutputFile != "" {
			// When writing to a file, create it and ensure it's closed properly
			file, err := os.Create(config.OutputFile)
			if err != nil {
				writeErr = fmt.Errorf("failed to create output file: %v", err)
				return
			}
			defer file.Close()
			outputWriter = file
		} else {
			// When writing to stdout, use the pipe writer for streaming output
			outputWriter = writer
		}

		// Execute the requested command and write results to the selected output
		switch config.Command.Name {
		case "genesisblock":
			block, blockErr := blockDump.GenesisBlock(ctx)
			if blockErr != nil {
				writeErr = fmt.Errorf("failed to get genesis block: %v", blockErr)
				return
			}
			writeErr = WriteEncoder(outputWriter, config.BinaryOutput, block)

		case "specificblock":
			if len(config.Command.Args) != 1 {
				writeErr = fmt.Errorf("specificblock requires exactly one argument")
				return
			}
			blockID, err := strconv.Atoi(config.Command.Args[0])
			if err != nil {
				writeErr = fmt.Errorf("invalid block ID: %v", err)
				return
			}
			block, err := blockDump.SpecificBlock(ctx, blockID)
			if err != nil {
				writeErr = fmt.Errorf("failed to get specific block: %v", err)
				return
			}
			writeErr = WriteEncoder(outputWriter, config.BinaryOutput, block)

		case "blockrange":
			if len(config.Command.Args) != 2 {
				writeErr = fmt.Errorf("blockrange requires exactly two arguments")
				return
			}
			start, err := strconv.Atoi(config.Command.Args[0])
			if err != nil {
				writeErr = fmt.Errorf("invalid start block ID: %v", err)
				return
			}
			end, err := strconv.Atoi(config.Command.Args[1])
			if err != nil {
				writeErr = fmt.Errorf("invalid end block ID: %v", err)
				return
			}
			blocks, err := blockDump.BlockRange(ctx, start, end)
			if err != nil {
				writeErr = fmt.Errorf("failed to get block range: %v", err)
				return
			}
			writeErr = WriteEncoder(outputWriter, config.BinaryOutput, blocks)

		case "randomblock":
			block, err := blockDump.RandomBlock(ctx)
			if err != nil {
				writeErr = fmt.Errorf("failed to get random block: %v", err)
				return
			}
			writeErr = WriteEncoder(outputWriter, config.BinaryOutput, block)

		case "randomblocksample":
			if len(config.Command.Args) != 1 {
				writeErr = fmt.Errorf("randomblocksample requires exactly one argument")
				return
			}
			sampleSize, err := strconv.Atoi(config.Command.Args[0])
			if err != nil {
				writeErr = fmt.Errorf("invalid sample size: %v", err)
				return
			}
			blocks, err := blockDump.RandomBlockSample(ctx, sampleSize)
			if err != nil {
				writeErr = fmt.Errorf("failed to get random block sample: %v", err)
				return
			}
			writeErr = WriteEncoder(outputWriter, config.BinaryOutput, blocks)

		case "allblocks":
			blocks, err := blockDump.AllBlocks(ctx)
			if err != nil {
				writeErr = fmt.Errorf("failed to get all blocks: %v", err)
				return
			}
			writeErr = WriteEncoder(outputWriter, config.BinaryOutput, blocks)

		default:
			writeErr = fmt.Errorf("unknown command: %s", config.Command.Name)
			return
		}
	}()

	// Return the reader end of the pipe, which will receive data as it's written
	return reader, nil
}

// setupRPCClient creates and configures an RPC client based on the given configuration
func setupRPCClient(config *RPCConfig) (IBasicBChainRPC, error) {
	// Create a new CLAM RPC client
	client := &CLAMBasicBChainRPC{}

	// Update the client configuration
	err := client.UpdateConfig(func() RPCConfig {
		return *config
	})
	if err != nil {
		return nil, fmt.Errorf("failed to configure RPC client: %v", err)
	}

	return client, nil
}

type IBlockIterator interface {
	// For efficient forward/backward iteration
	GetBlocksByRange(ctx context.Context, start, end int) iter.Seq2[string, error] // returns sequence of block hashes
}

const (
	// I haven't seen a RPC blockchain with a genesis block id other than 0
	// if there is one, we could always add a GenesisBlockID() method
	// to the IBasicBChainRPC interface
	GENESIS_BLOCK_ID = 0
)

// SendError attempts to send the error to the channel. If the channel is not ready to receive,
// it passes the error to the provided error handling function.
func SendError(errCh chan<- error, fallback func(error), err error) {
	select {
	case errCh <- err:
		// Successfully sent the error to the channel
	default:
		// Channel is not ready to receive, handle the error
		fallback(err)
	}
}

type ErrorChanneler interface {
	ErrorChannel() <-chan error
}

type Encoder interface {
	ErrorChanneler
	Encode() io.Reader
}

type Decoder[T any] interface {
	Decode(io.Reader) (T, error)
}

type SeqEncoder[T any] interface {
	Encoder
	Decoder[SeqEncoder[T]]
	ErrorChanneler // shared by Seq and Encoder
	Seq() iter.Seq[T]
}

type IBlockLookup interface {
	GetBlockByHash(ctx context.Context, hash string) (IBlock, error)
}

type IBlock interface {
	Encoder
	Decoder[IBlock]
	ID() int
	Hash() string
	Bytes() []byte
	Parent(context.Context, IBlockLookup) (IBlock, error)
	ParentHash() string
}

type Block struct {
	id               int
	hash             string
	bytes            []byte
	parent_hash      string
	error_channel    chan error
	error_channel_mu sync.Mutex
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

// PipeWriterPanic provides a robust error handling mechanism for pipe-based operations by
// ensuring errors are properly propagated through both a channel and the pipe itself. It
// allows for flexible handling of errors through an optional panic mechanism.
//
// When an error occurs in pipe writing operations, this function ensures the error is
// communicated in two ways:
//  1. Through an error channel for wider error awareness
//  2. Through the pipe writer's CloseWithError to notify the reading end
//
// The function uses SendError as its foundation, which attempts a non-blocking send of the
// error through the channel. If the channel cannot immediately receive the error (e.g., if
// it's full or has no receiver), SendError will execute a fallback that ensures the pipe
// reader is notified and optionally triggers a panic.
//
// Parameters:
//
//   - do_panic: A boolean flag that controls the severity of the error handling. When true,
//     the function will panic after ensuring error propagation. When false, it allows for
//     continued execution after error handling. This flexibility lets callers adjust the
//     response based on error severity.
//
//   - pipe_writer: A pointer to an io.PipeWriter representing the writing end of a pipe.
//     The function ensures any goroutine reading from the corresponding PipeReader will
//     be notified of the error through CloseWithError.
//
//   - errors: A write-only error channel (chan<- error) used as the primary mechanism for
//     error propagation throughout the system. This channel should typically be buffered
//     to reduce the chance of blocking.
//
//   - err: The error that triggered this error handling. This same error will be propagated
//     through both the channel and the pipe writer.
//
// The function guarantees that errors will be handled appropriately regardless of the
// channel's state, making it particularly useful in cleanup scenarios or when error
// handling must not block. It combines immediate local error handling (closing the pipe)
// with broader system error propagation (via the channel).
func PipeWriterPanic(do_panic bool, pipe_writer *io.PipeWriter, errors chan<- error, err error) {
	if pipe_writer == nil {
		panic(fmt.Errorf("nil pipe writer"))
	}

	if errors == nil {
		panic(fmt.Errorf("nil error channel"))
	}

	SendError(errors, func(err error) {
		// Ensure reader sees error
		pipe_writer.CloseWithError(err)
		if do_panic {
			panic(err)
		}
	}, err)
}

func (self *Block) get_error_channel() chan error {
	// Lock to ensure thread-safe channel access and creation
	self.error_channel_mu.Lock()
	defer self.error_channel_mu.Unlock()

	// Create a new channel if none exists
	if self.error_channel == nil {
		self.error_channel = make(chan error, 0) // unbuffered channel
	}

	return self.error_channel
}

func (self *Block) ErrorChannel() <-chan error {
	return self.get_error_channel()
}

func (self *Block) Encode() io.Reader {
	pipe_reader, pipe_writer := io.Pipe()
	error_channel := self.get_error_channel()

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
			PipeWriterPanic(true, pipe_writer, error_channel, err)
			return
		}

		// Write size prefix
		size := uint32(len(self.bytes))
		if err := binary.Write(pipe_writer, binary.LittleEndian, size); err != nil {
			PipeWriterPanic(true, pipe_writer, error_channel,
				fmt.Errorf("failed to write block size: %w", err))
			return
		}

		// Write block data
		_, err = pipe_writer.Write(data)
		if err != nil {
			PipeWriterPanic(true, pipe_writer, error_channel, err)
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

// IBlockFactory handles the creation of IBlock instances from different sources.
// It abstracts away the details of block construction and data fetching,
// providing a clean interface for creating blocks.
type IBlockFactory interface {
	// DecodeBlock constructs an IBlock from BSON-encoded data
	DecodeBlock(blockData io.Reader) (IBlock, error)

	// LookupBlockFromHash combines data fetching with block creation
	LookupBlockFromHash(ctx context.Context, hash string, rpc IBasicBChainRPC) (IBlock, error)
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
func (self *BlockFactory) LookupBlockFromHash(ctx context.Context, hash string, rpc IBasicBChainRPC) (IBlock, error) {
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

// BlockDump implements IBlockDump interface using blockchain RPC and iterator capabilities.
// It provides methods to access blocks in various ways: individually, in ranges,
// or randomly sampled.
type BlockDump struct {
	rpc           IBasicBChainRPC
	iterator      IBlockIterator
	block_factory IBlockFactory
}

// NewBlockDump creates a new BlockDump instance with the required dependencies.
func NewBlockDump(rpc IBasicBChainRPC, iterator IBlockIterator) (*BlockDump, error) {
	if rpc == nil {
		return nil, fmt.Errorf("rpc must not be nil")
	}
	if iterator == nil {
		return nil, fmt.Errorf("iterator must not be nil")
	}
	return &BlockDump{
		rpc:           rpc,
		iterator:      iterator,
		block_factory: NewBlockFactory(),
	}, nil
}

// GenesisBlock retrieves the first block in the blockchain.
// This implementation assumes GENESIS_BLOCK_ID is 0, as defined in the constants.
func (self *BlockDump) GenesisBlock(ctx context.Context) (IBlock, error) {
	// Get the hash of the genesis block
	hash, err := self.rpc.GetBlockhash(ctx, GENESIS_BLOCK_ID)
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
func (self *BlockDump) AllBlocks(ctx context.Context) (SeqEncoder[IBlock], error) {
	// Get the current block count
	count, err := self.rpc.GetBlockCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block count: %w", err)
	}

	// Get all blocks from genesis (0) to the latest block
	return self.BlockRange(ctx, GENESIS_BLOCK_ID, count)
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

func ChanSeq2[T any](seq iter.Seq2[T, error]) <-chan struct {
	Val T
	Err error
} {
	ch := make(chan struct {
		Val T
		Err error
	})
	go func() {
		defer close(ch)
		seq(func(val T, err error) bool {
			ch <- struct {
				Val T
				Err error
			}{Val: val, Err: err}
			return true
		})
	}()
	return ch
}

// BlockRange returns a sequence of blocks within the specified range [start, end].
// It uses the IBlockIterator for efficient block retrieval and implements streaming
// block fetching rather than loading all blocks into memory at once.
func (self *BlockDump) BlockRange(ctx context.Context, start, end int) (SeqEncoder[IBlock], error) {
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
		// Get block hashes for the range
		hashSeq := self.iterator.GetBlocksByRange(ctx, start, end)

		// Create a channel to receive hash/error pairs
		hashChan := ChanSeq2(hashSeq)

		// Process each hash in the sequence
		for result := range hashChan {
			// Check for hash retrieval errors
			if result.Err != nil {
				if !yield(nil, fmt.Errorf("failed to get block hash: %w", result.Err)) {
					return
				}
				continue
			}

			// Create block from hash using the factory
			block, err := self.block_factory.LookupBlockFromHash(ctx, result.Val, self.rpc)
			if err != nil {
				if !yield(nil, fmt.Errorf("failed to create block from hash %s: %w", result.Val, err)) {
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
func (self *BlockDump) RandomBlockSample(ctx context.Context, samplesize int) (SeqEncoder[IBlock], error) {
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

// CLAMBasicBChainRPC implements core RPC functionality for interacting with a CLAM node.
// It follows the JSON-RPC 1.0 protocol and error handling patterns from the CLAM Core codebase.
type CLAMBasicBChainRPC struct {
	config *RPCConfig
	client *http.Client
	reqID  atomic.Int64 // Counter for JSON-RPC request IDs, using atomic operations

	// Protects access to network resources
	mtx sync.Mutex
}

// CLAMRPCResponse matches CLAM Core's JSON-RPC response format.
// This struct is specific to the CLAM RPC implementation.
type CLAMRPCResponse struct {
	Version string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *CLAMRPCError   `json:"error,omitempty"`
}

// CLAMRPCError represents the error format used by CLAM Core's RPC interface.
// This is specific to CLAM's error handling approach.
type CLAMRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Common RPC error codes from CLAM Core's protocol.h
const (
	// Standard JSON-RPC 2.0 errors (unused in CLAM but kept for compatibility)
	CLAM_RPC_INVALID_REQUEST  = -32600
	CLAM_RPC_METHOD_NOT_FOUND = -32601
	CLAM_RPC_INVALID_PARAMS   = -32602
	CLAM_RPC_INTERNAL_ERROR   = -32603
	CLAM_RPC_PARSE_ERROR      = -32700

	// CLAM Core specific errors
	CLAM_RPC_MISC_ERROR              = -1  // std::exception thrown in command handling
	CLAM_RPC_FORBIDDEN_BY_SAFE_MODE  = -2  // Server is in safe mode
	CLAM_RPC_TYPE_ERROR              = -3  // Unexpected type was passed as parameter
	CLAM_RPC_INVALID_ADDRESS_OR_KEY  = -5  // Invalid address or key
	CLAM_RPC_OUT_OF_MEMORY           = -7  // Ran out of memory during operation
	CLAM_RPC_INVALID_PARAMETER       = -8  // Invalid, missing or duplicate parameter
	CLAM_RPC_DATABASE_ERROR          = -20 // Database error
	CLAM_RPC_DESERIALIZATION_ERROR   = -22 // Error parsing or validating structure in raw format
	CLAM_RPC_VERIFY_ERROR            = -25 // General error during transaction or block submission
	CLAM_RPC_VERIFY_REJECTED         = -26 // Transaction or block was rejected by network rules
	CLAM_RPC_VERIFY_ALREADY_IN_CHAIN = -27 // Transaction already in chain
	CLAM_RPC_IN_WARMUP               = -28 // Client still warming up
)

// NewCLAMBasicBChainRPC creates a new RPC client with the given configuration
func NewCLAMBasicBChainRPC(config *RPCConfig) (*CLAMBasicBChainRPC, error) {
	if err := validateConfig(&CommandLineConfig{RPCConfig: config}); err != nil {
		return nil, fmt.Errorf("invalid RPC configuration: %v", err)
	}

	rpc := &CLAMBasicBChainRPC{
		config: config,
		client: nil,
	}

	// Initialize the atomic counter
	rpc.reqID.Store(0)

	return rpc, nil
}

// UpdateConfig updates the RPC client configuration with the given values.
// This method is thread-safe and can be called concurrently.
func (self *CLAMBasicBChainRPC) UpdateConfig(update func() RPCConfig) error {
	self.mtx.Lock()
	defer self.mtx.Unlock()

	new_rpc_config := update()
	if err := validateConfig(&CommandLineConfig{RPCConfig: &new_rpc_config}); err != nil {
		return fmt.Errorf("invalid RPC configuration: %v", err)
	}

	// Update the RPC configuration
	self.config = &new_rpc_config

	// Create a new HTTP client with the updated configuration
	self.client = &http.Client{
		Timeout: self.config.ClientTimeout,
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: true,
			DisableKeepAlives:  false,
			MaxConnsPerHost:    10,
			ForceAttemptHTTP2:  false,
		},
	}

	return nil
}

// BasicConfig returns the default configuration of the RPC client
func (self *CLAMBasicBChainRPC) BasicConfig() RPCConfig {
	def := DefaultCommandLineConfig()
	return *def.RPCConfig
}

// GetBestBlockhash retrieves the hash of the best (most recent) block in the CLAM blockchain.
// The "best" block is the tip of the most-work chain that the node currently knows about.
func (self *CLAMBasicBChainRPC) GetBestBlockhash(ctx context.Context) (string, error) {
	// Make the RPC call to the CLAM node
	// The getbestblockhash method takes no parameters
	response, err := self.call("getbestblockhash", nil)
	if err != nil {
		return "", fmt.Errorf("failed to get best block hash: %w", err)
	}

	// The result should be a string containing the block hash
	var blockhash string
	if err := json.Unmarshal(response.Result, &blockhash); err != nil {
		return "", fmt.Errorf("failed to parse best block hash: %w", err)
	}

	return blockhash, nil
}

// GetBlockCount retrieves the current number of blocks in the CLAM blockchain.
// This represents the height of the blockchain's longest valid chain.
func (self *CLAMBasicBChainRPC) GetBlockCount(ctx context.Context) (int, error) {
	// Make the RPC call to the CLAM node
	// The getblockcount method takes no parameters
	response, err := self.call("getblockcount", nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get block count: %w", err)
	}

	// The result should be a number
	var count int
	if err := json.Unmarshal(response.Result, &count); err != nil {
		return 0, fmt.Errorf("failed to parse block count: %w", err)
	}

	return count, nil
}

// GetBlockJSON retrieves detailed block information in JSON format.
// This method returns the most verbose form of block data available from the CLAM node.
// The verbosity level is set to 2 to get all available transaction details.
func (self *CLAMBasicBChainRPC) GetBlockJSON(ctx context.Context, blockhash string) (map[string]interface{}, error) {
	// Create parameters array: [blockhash, verbosity]
	// Verbosity = 2 gives us the most detailed block information including full transaction data
	params := []interface{}{blockhash, 2}

	// Make the RPC call to the CLAM node
	response, err := self.call("getblock", params)
	if err != nil {
		return nil, fmt.Errorf("failed to get block JSON for hash %s: %w", blockhash, err)
	}

	// Parse the response into a map structure
	var blockData map[string]interface{}
	if err := json.Unmarshal(response.Result, &blockData); err != nil {
		return nil, fmt.Errorf("failed to parse block JSON for hash %s: %w", blockhash, err)
	}

	return blockData, nil
}

// GetBlockReader retrieves raw block data from the CLAM blockchain and returns it as an io.Reader.
// This method is crucial for efficient handling of block data, especially for large blocks,
// as it allows streaming the data rather than loading it all into memory at once.
func (self *CLAMBasicBChainRPC) GetBlockReader(ctx context.Context, blockhash string) (io.Reader, error) {
	// First, we need to get the raw block data in hexadecimal format
	// We use verbosity level 0 with getblock to get the raw block data
	params := []interface{}{blockhash, 0}

	response, err := self.call("getblock", params)
	if err != nil {
		return nil, fmt.Errorf("failed to get raw block data for hash %s: %w", blockhash, err)
	}

	// The response contains a hex string of the block data
	// We need to extract it and decode it
	var hexData string
	if err := json.Unmarshal(response.Result, &hexData); err != nil {
		return nil, fmt.Errorf("failed to parse raw block hex for hash %s: %w", blockhash, err)
	}

	// Decode the hex string into raw bytes
	rawData, err := hex.DecodeString(hexData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block hex data for hash %s: %w", blockhash, err)
	}

	// Return a reader that can stream the raw block data
	return bytes.NewReader(rawData), nil
}

// GetBlockhash converts a block height into its corresponding block hash.
// In blockchain systems, while block heights are sequential numbers, the actual
// unique identifier for a block is its hash. This method helps bridge that gap.
func (self *CLAMBasicBChainRPC) GetBlockhash(ctx context.Context, blockid int) (string, error) {
	// Create parameters array with just the block height
	// The getblockhash RPC method expects a single integer parameter
	params := []interface{}{blockid}

	// Make the RPC call to the CLAM node
	response, err := self.call("getblockhash", params)
	if err != nil {
		return "", fmt.Errorf("failed to get block hash for height %d: %w", blockid, err)
	}

	// The response contains the block hash as a string
	var blockhash string
	if err := json.Unmarshal(response.Result, &blockhash); err != nil {
		return "", fmt.Errorf("failed to parse block hash for height %d: %w", blockid, err)
	}

	// Validate the block hash format
	// CLAM block hashes, like Bitcoin's, are 64-character hexadecimal strings
	if len(blockhash) != 64 {
		return "", fmt.Errorf("invalid block hash length for height %d: got %d characters, expected 64",
			blockid, len(blockhash))
	}

	// Optional: Verify that the string contains only valid hexadecimal characters
	if _, err := hex.DecodeString(blockhash); err != nil {
		return "", fmt.Errorf("malformed block hash for height %d: %w", blockid, err)
	}

	return blockhash, nil
}

// IsValidBlockID checks whether a given block height exists in the blockchain.
// This validation is crucial for preventing errors when working with block IDs
// and maintaining the reliability of block retrieval operations.
func (self *CLAMBasicBChainRPC) IsValidBlockID(ctx context.Context, id int) (bool, error) {
	// First, let's check if the ID is non-negative, as block heights cannot be negative
	if id < 0 {
		return false, nil
	}

	// Get the current blockchain height to know the valid range
	response, err := self.call("getblockcount", nil)
	if err != nil {
		return false, fmt.Errorf("failed to get blockchain height while validating block ID %d: %w", id, err)
	}

	var blockCount int
	if err := json.Unmarshal(response.Result, &blockCount); err != nil {
		return false, fmt.Errorf("failed to parse blockchain height while validating block ID %d: %w", id, err)
	}

	// A block ID is valid if it's between 0 and the current blockchain height
	return id <= blockCount, nil
}

// call performs a JSON-RPC call with automatic error handling and retries.
// It follows CLAM Core's retry and backoff strategy for failed requests.
func (self *CLAMBasicBChainRPC) call(method string, params []interface{}) (*CLAMRPCResponse, error) {
	self.mtx.Lock()
	defer self.mtx.Unlock()

	// Increment request ID atomically using atomic.Int64
	id := self.reqID.Add(1)

	// Construct RPC request following CLAM's JSON-RPC 1.0 format
	rpcReq := struct {
		Version string        `json:"jsonrpc"`
		ID      int64         `json:"id"`
		Method  string        `json:"method"`
		Params  []interface{} `json:"params"`
	}{
		Version: "1.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	reqBody, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("%d: error marshaling request: %v", CLAM_RPC_INTERNAL_ERROR, err)
	}

	url := fmt.Sprintf("http://%s:%d", self.config.Connect, self.config.Port)

	var lastErr error
	for attempt := 0; attempt <= self.config.RetryLimit; attempt++ {
		if attempt > 0 {
			backoff := calculateBackoff(attempt, self.config.RetryBackoffMin, self.config.RetryBackoffMax)
			time.Sleep(backoff)
		}

		req, err := http.NewRequest("POST", url, bytes.NewReader(reqBody))
		if err != nil {
			lastErr = fmt.Errorf("%d: error creating request: %v", CLAM_RPC_INTERNAL_ERROR, err)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		if self.config.User != "" {
			req.SetBasicAuth(self.config.User, self.config.Password)
		}

		resp, err := self.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%d: error performing request: %v", CLAM_RPC_INTERNAL_ERROR, err)
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = fmt.Errorf("%d: error reading response: %v", CLAM_RPC_INTERNAL_ERROR, err)
			continue
		}

		// Handle HTTP errors following CLAM's error mapping
		if resp.StatusCode != http.StatusOK {
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				lastErr = fmt.Errorf("%d: incorrect RPC credentials", CLAM_RPC_INVALID_REQUEST)
			case http.StatusForbidden:
				lastErr = fmt.Errorf("%d: forbidden by safe mode", CLAM_RPC_FORBIDDEN_BY_SAFE_MODE)
			case http.StatusNotFound:
				lastErr = fmt.Errorf("%d: method not found", CLAM_RPC_METHOD_NOT_FOUND)
			default:
				lastErr = fmt.Errorf("%d: HTTP error %d: %s", CLAM_RPC_INTERNAL_ERROR,
					resp.StatusCode, string(body))
			}
			continue
		}

		var rpcResp CLAMRPCResponse
		if err := json.Unmarshal(body, &rpcResp); err != nil {
			lastErr = fmt.Errorf("%d: error parsing response: %v", CLAM_RPC_PARSE_ERROR, err)
			continue
		}

		if rpcResp.Error != nil {
			lastErr = fmt.Errorf("%d: RPC error: %s", rpcResp.Error.Code, rpcResp.Error.Message)
			continue
		}

		return &rpcResp, nil
	}

	return nil, fmt.Errorf("max retries exceeded, last error: %v", lastErr)
}

func rand_Float64() float64 {
	// read a random 64-bit float from crypto/rand.Reader
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b))
}

// calculateBackoff determines the backoff duration for retries.
// The algorithm matches CLAM Core's exponential backoff with jitter strategy.
func calculateBackoff(attempt int, min, max time.Duration) time.Duration {
	// Use exponential backoff: min * 2^attempt
	backoff := min * time.Duration(1<<uint(attempt))
	if backoff > max {
		backoff = max
	}

	// Add jitter (±20%) to prevent thundering herd
	// Matches CLAM Core's retry jitter implementation
	jitter := time.Duration(rand_Float64()*0.4*float64(backoff)) - (backoff / 5)
	return backoff + jitter
}

// BlockIterator handles efficient iteration over blockchain blocks by managing
// block hash retrieval in ranges. It uses the RPC client for actual data fetching.
type BlockIterator struct {
	rpc IBasicBChainRPC
}

// NewBlockIterator creates a new BlockIterator instance that will use the provided
// RPC client for block hash retrieval operations.
func NewBlockIterator(rpc IBasicBChainRPC) *BlockIterator {
	return &BlockIterator{
		rpc: rpc,
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

// BlockSequence implements SeqEncoder[IBlock] to provide a way to encode and decode
// sequences of blocks. It handles both the sequential nature of blocks and their
// encoding/decoding operations while maintaining consistent error handling patterns.
type BlockSequence struct {
	seq              iter.Seq2[IBlock, error]
	error_channel    chan error
	error_channel_mu sync.Mutex
}

// NewBlockSequenceEncoding creates a new BlockSequence from an iterator sequence.
// The error channel is created lazily when needed, following the pattern used
// in the Block implementation.
func NewBlockSequenceEncoding(seq iter.Seq2[IBlock, error]) SeqEncoder[IBlock] {
	return &BlockSequence{
		seq: seq,
	}
}

// get_error_channel ensures thread-safe access to the error channel,
// creating it if it doesn't exist. This matches the pattern used in Block.
func (self *BlockSequence) get_error_channel() chan error {
	self.error_channel_mu.Lock()
	defer self.error_channel_mu.Unlock()

	if self.error_channel == nil {
		self.error_channel = make(chan error) // Unbuffered channel
	}

	return self.error_channel
}

// ErrorChannel returns a read-only channel for error notifications,
// ensuring encapsulation of the error channel.
func (self *BlockSequence) ErrorChannel() <-chan error {
	return self.get_error_channel()
}

// Seq returns the underlying block sequence iterator, propagating errors
// through the error channel using SendError.
func (self *BlockSequence) Seq() iter.Seq[IBlock] {
	return func(yield func(IBlock) bool) {
		self.seq(func(block IBlock, err error) bool {
			if err != nil {
				// Use SendError for consistent error handling
				SendError(self.get_error_channel(), func(err error) {
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
	error_channel := self.get_error_channel()

	go func() {
		defer pipe_writer.Close()
		defer close(error_channel)

		self.seq(func(block IBlock, err error) bool {
			if err != nil {
				PipeWriterPanic(true, pipe_writer, error_channel,
					fmt.Errorf("sequence error: %w", err))
				return false
			}

			if block == nil {
				PipeWriterPanic(true, pipe_writer, error_channel,
					fmt.Errorf("nil block in sequence"))
				return false
			}

			// Get block data
			reader := block.Encode()

			// Copy block data and flush after each block
			_, err = io.Copy(pipe_writer, reader)
			if err != nil {
				PipeWriterPanic(true, pipe_writer, error_channel,
					fmt.Errorf("failed to write block data: %w", err))
				return false
			}

			// Check for block encoding errors
			select {
			case err := <-block.ErrorChannel():
				if err != nil {
					PipeWriterPanic(true, pipe_writer, error_channel,
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
func (self *BlockSequence) Decode(r io.Reader) (SeqEncoder[IBlock], error) {
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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/222-crypto/blockdump/v2/block"
	"github.com/222-crypto/blockdump/v2/config"
	"github.com/222-crypto/blockdump/v2/encoding"
	"github.com/222-crypto/blockdump/v2/rpc"
	"github.com/222-crypto/blockdump/v2/rpc/clamrpc"
)

const USAGE = `
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
`

// Command represents the subcommand to execute
type Command struct {
	Name string   // The name of the command (e.g., "genesisblock", "allblocks", etc.)
	Args []string // Any additional arguments for the command
}

// CommandLineConfig holds all command-line configuration options
type CommandLineConfig struct {
	*rpc.RPCConfig // Embedded RPC configuration

	// Output Settings
	BinaryOutput bool   // Whether to output in binary format (-b flag)
	OutputFile   string // File to write output to (-f flag), empty means stdout

	// Command to execute (parsed from remaining arguments)
	Command *Command
}

// DefaultCommandLineConfig returns a CommandLineConfig with default values set
func DefaultCommandLineConfig() *CommandLineConfig {
	rpc_config := rpc.DefaultRPCConfig()

	return &CommandLineConfig{
		RPCConfig:    &rpc_config,
		BinaryOutput: false, // Default to text output
		Command:      nil,   // Must be specified by user
	}
}

// replacePlaceHolders replaces placeholder values in the RPC configuration
// with their default values if placeholder values are detected.
func replacePlaceHolders(rpc_config *rpc.RPCConfig) {
	default_config := DefaultCommandLineConfig()

	if rpc_config.User == "${BLOCKDUMP_RPC_USER}" {
		rpc_config.User = default_config.RPCConfig.User
	}

	if rpc_config.Password == "${BLOCKDUMP_RPC_PASSWORD}" {
		rpc_config.Password = default_config.RPCConfig.Password
	}
}

// parseFlags handles custom flag parsing to support both single and double dash flags
func parseFlags(args []string) (*CommandLineConfig, []string, error) {
	config := DefaultCommandLineConfig()
	remainingArgs := make([]string, 0)

	i := 0
	for i < len(args) {
		arg := args[i]

		// Handle help flag specially
		if arg == "-h" || arg == "--help" {
			printUsage()
			os.Exit(0)
		}

		// Handle flags that start with - or --
		if strings.HasPrefix(arg, "-") {
			// Strip leading dashes and handle the flag
			flag := strings.TrimLeft(arg, "-")

			// Handle combined flags (only supported with single dash)
			if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
				if flag == "fb" || flag == "bf" {
					config.BinaryOutput = true
					i++
					if i >= len(args) {
						return nil, nil, fmt.Errorf("missing filename after -%s flag", flag)
					}
					config.OutputFile = args[i]
					i++
					continue
				}
			}

			// Handle flags with values after =
			if strings.Contains(flag, "=") {
				parts := strings.SplitN(flag, "=", 2)
				flag = parts[0]
				value := parts[1]

				// Handle RPC flags
				if strings.HasPrefix(flag, "rpc") {
					if err := setRPCOption(config, flag, value); err != nil {
						return nil, nil, err
					}
					i++
					continue
				}
			}

			// Handle standalone flags
			switch flag {
			case "f", "file":
				i++
				if i >= len(args) {
					return nil, nil, fmt.Errorf("missing filename after %s flag", arg)
				}
				config.OutputFile = args[i]
			case "b", "binary":
				config.BinaryOutput = true
			case "help":
				printUsage()
				os.Exit(0)
			default:
				if strings.HasPrefix(flag, "rpc") {
					// Handle RPC flags without = (assuming value is next argument)
					i++
					if i >= len(args) {
						return nil, nil, fmt.Errorf("missing value for %s flag", arg)
					}
					if err := setRPCOption(config, flag, args[i]); err != nil {
						return nil, nil, err
					}
				} else {
					return nil, nil, fmt.Errorf("unknown flag: %s", arg)
				}
			}
		} else {
			// Non-flag argument - add to remaining args
			remainingArgs = append(remainingArgs, arg)
		}
		i++
	}

	replacePlaceHolders(config.RPCConfig)

	return config, remainingArgs, nil
}

// setRPCOption handles setting individual RPC configuration options
func setRPCOption(config *CommandLineConfig, flag string, value string) error {
	switch flag {
	case "rpcconnect":
		config.Connect = value
	case "rpcport":
		port, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid port number: %s", value)
		}
		config.Port = port
	case "rpcuser":
		config.User = value
	case "rpcpassword":
		config.Password = value
	case "rpcclienttimeout":
		timeout, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid timeout value: %s", value)
		}
		config.ClientTimeout = time.Duration(timeout) * time.Second
	case "rpcretrylimit":
		limit, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid retry limit: %s", value)
		}
		config.RetryLimit = limit
	case "rpcretrybackoffmax":
		max, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid max backoff: %s", value)
		}
		config.RetryBackoffMax = time.Duration(max) * time.Second
	case "rpcretrybackoffmin":
		min, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid min backoff: %s", value)
		}
		config.RetryBackoffMin = time.Duration(min) * time.Second
	default:
		return fmt.Errorf("unknown RPC flag: %s", flag)
	}
	return nil
}

// ParseConfig parses command-line arguments and returns a filled Config
func ParseConfig() (*CommandLineConfig, error) {
	// Skip the program name
	args := os.Args[1:]

	// Parse flags and get remaining arguments
	config, remainingArgs, err := parseFlags(args)
	if err != nil {
		return nil, err
	}

	// Parse the command from remaining arguments
	if len(remainingArgs) == 0 {
		printUsage()
		return nil, fmt.Errorf("no command specified")
	}

	// Parse the command
	cmd, err := parseCommand(remainingArgs)
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

func printUsage() {
	fmt.Fprintf(os.Stderr, USAGE)
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

// unparseCommand converts a Command struct back into a string slice of arguments.
// It reconstructs the original command-line arguments that would have created this Command.
// The returned slice will have the command name as the first element,
// followed by any arguments in their original order.
func unparseCommand(cmd *Command) []string {
	// If cmd is nil, return an empty slice to avoid panic
	if cmd == nil {
		return []string{}
	}

	// Create a new slice with capacity for command name + all arguments
	// This is more efficient than appending as we know the final size
	result := make([]string, 0, len(cmd.Args)+1)

	// First element is always the command name
	result = append(result, cmd.Name)

	// Add all arguments in their original order
	result = append(result, cmd.Args...)

	return result
}

// validateConfig performs validation on the entire configuration
func validateConfig(clc *CommandLineConfig) error {
	// Validate the RPC configuration
	rpc_config_validation_err := config.ValidateRPCConfig(clc.RPCConfig)
	if rpc_config_validation_err != nil {
		return fmt.Errorf("invalid RPC configuration: %w", rpc_config_validation_err)
	}

	// Validate the command
	if clc.Command == nil {
		return fmt.Errorf("command must be specified")
	}
	args := unparseCommand(clc.Command)
	if _, err := parseCommand(args); err != nil {
		return fmt.Errorf("validation error when parsing command: %w", err)
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
	rpcClient, err := clamrpc.NewCLAMBasicBChainRPC(config.RPCConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to setup RPC client: %v", err)
	}

	// Create block iterator using the RPC client
	blockIterator := block.NewBlockIterator(rpcClient)

	// Create BlockDump instance that will handle the command execution
	blockDump, err := block.NewBlockDump(rpcClient, blockIterator)
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
			writeErr = encoding.WriteEncoder(outputWriter, config.BinaryOutput, block)

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
			writeErr = encoding.WriteEncoder(outputWriter, config.BinaryOutput, block)

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
			writeErr = encoding.WriteEncoder(outputWriter, config.BinaryOutput, blocks)

		case "randomblock":
			block, err := blockDump.RandomBlock(ctx)
			if err != nil {
				writeErr = fmt.Errorf("failed to get random block: %v", err)
				return
			}
			writeErr = encoding.WriteEncoder(outputWriter, config.BinaryOutput, block)

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
			writeErr = encoding.WriteEncoder(outputWriter, config.BinaryOutput, blocks)

		case "allblocks":
			blocks, err := blockDump.AllBlocks(ctx)
			if err != nil {
				writeErr = fmt.Errorf("failed to get all blocks: %v", err)
				return
			}
			writeErr = encoding.WriteEncoder(outputWriter, config.BinaryOutput, blocks)

		default:
			writeErr = fmt.Errorf("unknown command: %s", config.Command.Name)
			return
		}
	}()

	// Return the reader end of the pipe, which will receive data as it's written
	return reader, nil
}

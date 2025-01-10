package clamrpc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/222-crypto/blockdump/v2/config"
	"github.com/222-crypto/blockdump/v2/rpc"
	"github.com/222-crypto/blockdump/v2/rpc/utils/retry"
)

// NewCLAMBasicBChainRPC creates and configures an RPC client based on the given configuration
func NewCLAMBasicBChainRPC(config *rpc.RPCConfig) (rpc.IBasicBChainRPC, error) {
	// Create a new CLAM RPC client
	client := &CLAMBasicBChainRPC{}

	// Update the client configuration
	err := client.UpdateConfig(func() rpc.RPCConfig {
		return *config
	})
	if err != nil {
		return nil, fmt.Errorf("failed to configure RPC client: %v", err)
	}

	// Initialize the atomic counter
	client.reqID.Store(0)

	return client, nil
}

// CLAMBasicBChainRPC implements core RPC functionality for interacting with a CLAM node.
// It follows the JSON-RPC 1.0 protocol and error handling patterns from the CLAM Core codebase.
type CLAMBasicBChainRPC struct {
	config *rpc.RPCConfig
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

// UpdateConfig updates the RPC client configuration with the given values.
// This method is thread-safe and can be called concurrently.
func (self *CLAMBasicBChainRPC) UpdateConfig(update func() rpc.RPCConfig) error {
	self.mtx.Lock()
	defer self.mtx.Unlock()

	new_rpc_config := update()
	if err := config.ValidateRPCConfig(&new_rpc_config); err != nil {
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
func (self *CLAMBasicBChainRPC) BasicConfig() rpc.RPCConfig {
	return rpc.DefaultRPCConfig()
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
			backoff := retry.CalculateBackoff(attempt, self.config.RetryBackoffMin, self.config.RetryBackoffMax)
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

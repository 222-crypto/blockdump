package clamrpc

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

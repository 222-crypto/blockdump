package constants

const (
	// I haven't seen a RPC blockchain with a genesis block id other than 0
	// if there is one, we could always add a GenesisBlockID() method
	// to the IBasicBChainRPC interface
	GENESIS_BLOCK_ID = 0
)

package block

import "context"

type IBlockLookup interface {
	GetBlockByHash(ctx context.Context, hash string) (IBlock, error)
}

package market

import (
	"context"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type NonceManager struct {
	client  *ethclient.Client
	address common.Address
	mu      sync.Mutex
	nonce   uint64
	synced  bool
}

func NewNonceManager(rpcURL string, addressHex string) (*NonceManager, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, err
	}
	
	return &NonceManager{
		client:  client,
		address: common.HexToAddress(addressHex),
	}, nil
}

// GetNextNonce returns the next valid nonce.
// It syncs with the chain on the first call, then increments locally.
func (nm *NonceManager) GetNextNonce() (uint64, error) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if !nm.synced {
		// Sync with Pending state on chain
		nonce, err := nm.client.PendingNonceAt(context.Background(), nm.address)
		if err != nil {
			return 0, err
		}
		nm.nonce = nonce
		nm.synced = true
	}

	// Return current and increment
	next := nm.nonce
	nm.nonce++
	return next, nil
}

// Reset forces a re-sync with the chain (used when errors occur)
func (nm *NonceManager) Reset() {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.synced = false
}

// GetGasPrice suggests a gas price (Simple implementation, usually EIP-1559 needed)
func (nm *NonceManager) GetGasPrice() (*big.Int, error) {
	return nm.client.SuggestGasPrice(context.Background())
}

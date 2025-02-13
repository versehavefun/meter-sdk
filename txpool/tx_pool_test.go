// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package txpool

import (
	"testing"
	"time"

	"github.com/inconshreveable/log15"
	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/genesis"
	"github.com/meterio/meter-pov/lvldb"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/state"
	"github.com/meterio/meter-pov/tx"
	Tx "github.com/meterio/meter-pov/tx"
	"github.com/stretchr/testify/assert"
)

func init() {
	log15.Root().SetHandler(log15.DiscardHandler())
}

func newPool() *TxPool {
	kv, _ := lvldb.NewMem()
	chain := newChain(kv)
	return New(chain, state.NewCreator(kv), Options{
		Limit:           10,
		LimitPerAccount: 2,
		MaxLifetime:     time.Hour,
	})
}
func TestNewClose(t *testing.T) {
	pool := newPool()
	defer pool.Close()
}

func TestSubscribeNewTx(t *testing.T) {
	pool := newPool()
	defer pool.Close()

	b1 := new(block.Builder).
		ParentID(pool.chain.GenesisBlock().Header().ID()).
		Timestamp(uint64(time.Now().Unix())).
		TotalScore(100).
		GasLimit(10000000).
		StateRoot(pool.chain.GenesisBlock().Header().StateRoot()).
		Build()
	qc := block.QuorumCert{QCHeight: 1, QCRound: 1, EpochID: 0}
	b1.SetQC(&qc)
	pool.chain.AddBlock(b1, nil, true)

	txCh := make(chan *TxEvent)

	pool.SubscribeTxEvent(txCh)

	tx := newTx(pool.chain.Tag(), nil, 21000, tx.BlockRef{}, 100, nil, genesis.DevAccounts()[0])
	assert.Nil(t, pool.Add(tx))

	v := true
	assert.Equal(t, &TxEvent{tx, &v}, <-txCh)
}

func TestWashTxs(t *testing.T) {
	pool := newPool()
	defer pool.Close()
	txs, _, err := pool.wash(pool.chain.BestBlock().Header())
	assert.Nil(t, err)
	assert.Zero(t, len(txs))
	assert.Zero(t, len(pool.Executables()))

	tx := newTx(pool.chain.Tag(), nil, 21000, tx.BlockRef{}, 100, nil, genesis.DevAccounts()[0])
	assert.Nil(t, pool.Add(tx))

	txs, _, err = pool.wash(pool.chain.BestBlock().Header())
	assert.Nil(t, err)
	assert.Equal(t, Tx.Transactions{tx}, txs)

	b1 := new(block.Builder).
		ParentID(pool.chain.GenesisBlock().Header().ID()).
		Timestamp(uint64(time.Now().Unix())).
		TotalScore(100).
		GasLimit(10000000).
		StateRoot(pool.chain.GenesisBlock().Header().StateRoot()).
		Build()
	qc := block.QuorumCert{QCHeight: 1, QCRound: 1, EpochID: 0}
	b1.SetQC(&qc)
	pool.chain.AddBlock(b1, nil, true)

	txs, _, err = pool.wash(pool.chain.BestBlock().Header())
	assert.Nil(t, err)
	assert.Equal(t, Tx.Transactions{tx}, txs)
}

func TestAdd(t *testing.T) {
	pool := newPool()
	defer pool.Close()
	b1 := new(block.Builder).
		ParentID(pool.chain.GenesisBlock().Header().ID()).
		Timestamp(uint64(time.Now().Unix())).
		TotalScore(100).
		GasLimit(10000000).
		StateRoot(pool.chain.GenesisBlock().Header().StateRoot()).
		Build()
	qc := block.QuorumCert{QCHeight: 1, QCRound: 1, EpochID: 0}
	b1.SetQC(&qc)
	pool.chain.AddBlock(b1, nil, true)
	acc := genesis.DevAccounts()[0]

	dupTx := newTx(pool.chain.Tag(), nil, 21000, tx.BlockRef{}, 100, nil, acc)

	tests := []struct {
		tx     *tx.Transaction
		errStr string
	}{
		{newTx(pool.chain.Tag()+1, nil, 21000, tx.BlockRef{}, 100, nil, acc), "bad tx: chain tag mismatch"},
		{dupTx, ""},
		{dupTx, ""},
	}

	for _, tt := range tests {
		err := pool.Add(tt.tx)
		if tt.errStr == "" {
			assert.Nil(t, err)
		} else {
			assert.Equal(t, tt.errStr, err.Error())
		}
	}

	tests = []struct {
		tx     *tx.Transaction
		errStr string
	}{
		{newTx(pool.chain.Tag(), nil, 21000, tx.NewBlockRef(200), 100, nil, acc), "tx rejected: tx is not executable"},
		{newTx(pool.chain.Tag(), nil, 21000, tx.BlockRef{}, 100, &meter.Bytes32{1}, acc), "tx rejected: tx is not executable"},
	}

	for _, tt := range tests {
		err := pool.StrictlyAdd(tt.tx)
		if tt.errStr == "" {
			assert.Nil(t, err)
		} else {
			assert.Equal(t, tt.errStr, err.Error())
		}
	}
}

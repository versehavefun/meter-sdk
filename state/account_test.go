// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package state

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/meter-pov/lvldb"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/trie"
	"github.com/stretchr/testify/assert"
)

func M(a ...interface{}) []interface{} {
	return a
}

func TestAccount(t *testing.T) {
	assert.True(t, emptyAccount().IsEmpty())

	acc := emptyAccount()
	acc.Balance = big.NewInt(1)
	assert.False(t, acc.IsEmpty())
	acc = emptyAccount()
	acc.CodeHash = []byte{1}
	assert.False(t, acc.IsEmpty())

	acc = emptyAccount()
	acc.Energy = big.NewInt(1)
	assert.False(t, acc.IsEmpty())

	acc = emptyAccount()
	acc.StorageRoot = []byte{1}
	assert.False(t, acc.IsEmpty())
}

func newTrie() *trie.SecureTrie {
	kv, _ := lvldb.NewMem()
	trie, _ := trie.NewSecure(meter.Bytes32{}, kv, 0)
	return trie
}
func TestTrie(t *testing.T) {
	trie := newTrie()

	addr := meter.BytesToAddress([]byte("account1"))
	assert.Equal(t,
		M(loadAccount(trie, addr)),
		[]interface{}{emptyAccount(), nil},
		"should load an empty account")

	acc1 := Account{
		big.NewInt(1),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		[]byte("master"),
		[]byte("code hash"),
		[]byte("storage root"),
	}
	saveAccount(trie, addr, &acc1)
	assert.Equal(t,
		M(loadAccount(trie, addr)),
		[]interface{}{&acc1, nil})

	saveAccount(trie, addr, emptyAccount())
	assert.Equal(t,
		M(trie.TryGet(addr[:])),
		[]interface{}{[]byte(nil), nil},
		"empty account should be deleted")
}

func TestStorageTrie(t *testing.T) {
	trie := newTrie()

	key := meter.BytesToBytes32([]byte("key"))
	assert.Equal(t,
		M(loadStorage(trie, key)),
		[]interface{}{rlp.RawValue(nil), nil})

	value := rlp.RawValue("value")
	saveStorage(trie, key, value)
	assert.Equal(t,
		M(loadStorage(trie, key)),
		[]interface{}{value, nil})

	saveStorage(trie, key, nil)
	assert.Equal(t,
		M(trie.TryGet(key[:])),
		[]interface{}{[]byte(nil), nil},
		"empty storage value should be deleted")
}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package state

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/meter-pov/lvldb"
	"github.com/meterio/meter-pov/meter"
	"github.com/stretchr/testify/assert"
)

func TestStateReadWrite(t *testing.T) {
	kv, _ := lvldb.NewMem()
	state, _ := New(meter.Bytes32{}, kv)

	addr := meter.BytesToAddress([]byte("account1"))
	storageKey := meter.BytesToBytes32([]byte("storageKey"))

	assert.False(t, state.Exists(addr))
	assert.Equal(t, state.GetBalance(addr), &big.Int{})
	assert.Equal(t, state.GetCode(addr), []byte(nil))
	assert.Equal(t, state.GetCodeHash(addr), meter.Bytes32{})
	assert.Equal(t, state.GetStorage(addr, storageKey), meter.Bytes32{})

	// make account not empty
	state.SetBalance(addr, big.NewInt(1))
	assert.Equal(t, state.GetBalance(addr), big.NewInt(1))

	state.SetMaster(addr, meter.BytesToAddress([]byte("master")))
	assert.Equal(t, meter.BytesToAddress([]byte("master")), state.GetMaster(addr))

	state.SetCode(addr, []byte("code"))
	assert.Equal(t, state.GetCode(addr), []byte("code"))
	assert.Equal(t, state.GetCodeHash(addr), meter.Bytes32(crypto.Keccak256Hash([]byte("code"))))

	assert.Equal(t, state.GetStorage(addr, storageKey), meter.Bytes32{})
	state.SetStorage(addr, storageKey, meter.BytesToBytes32([]byte("storageValue")))
	assert.Equal(t, state.GetStorage(addr, storageKey), meter.BytesToBytes32([]byte("storageValue")))

	assert.True(t, state.Exists(addr))

	// delete account
	state.Delete(addr)
	assert.False(t, state.Exists(addr))
	assert.Equal(t, state.GetBalance(addr), &big.Int{})
	assert.Equal(t, state.GetMaster(addr), meter.Address{})
	assert.Equal(t, state.GetCode(addr), []byte(nil))
	assert.Equal(t, state.GetCodeHash(addr), meter.Bytes32{})

	assert.Nil(t, state.Err(), "error is not expected")

}

func TestStateRevert(t *testing.T) {
	kv, _ := lvldb.NewMem()
	state, _ := New(meter.Bytes32{}, kv)

	addr := meter.BytesToAddress([]byte("account1"))
	storageKey := meter.BytesToBytes32([]byte("storageKey"))

	values := []struct {
		balance *big.Int
		code    []byte
		storage meter.Bytes32
	}{
		{big.NewInt(1), []byte("code1"), meter.BytesToBytes32([]byte("v1"))},
		{big.NewInt(2), []byte("code2"), meter.BytesToBytes32([]byte("v2"))},
		{big.NewInt(3), []byte("code3"), meter.BytesToBytes32([]byte("v3"))},
	}

	var chk int
	for _, v := range values {
		chk = state.NewCheckpoint()
		state.SetBalance(addr, v.balance)
		state.SetCode(addr, v.code)
		state.SetStorage(addr, storageKey, v.storage)
	}

	for i := range values {
		v := values[len(values)-i-1]
		assert.Equal(t, state.GetBalance(addr), v.balance)
		assert.Equal(t, state.GetCode(addr), v.code)
		assert.Equal(t, state.GetCodeHash(addr), meter.Bytes32(crypto.Keccak256Hash(v.code)))
		assert.Equal(t, state.GetStorage(addr, storageKey), v.storage)
		state.RevertTo(chk)
		chk--
	}
	assert.False(t, state.Exists(addr))
	assert.Nil(t, state.Err(), "error is not expected")

	//
	state, _ = New(meter.Bytes32{}, kv)
	assert.Equal(t, state.NewCheckpoint(), 1)
	state.RevertTo(0)
	assert.Equal(t, state.NewCheckpoint(), 0)

}

/*
func TestEnergy(t *testing.T) {
	kv, _ := lvldb.NewMem()
	st, _ := New(meter.Bytes32{}, kv)

	acc := meter.BytesToAddress([]byte("a1"))

	time1 := uint64(1000)

	vetBal := big.NewInt(1e18)
	st.SetBalance(acc, vetBal)
	st.SetEnergy(acc, &big.Int{})

	bal1 := st.GetEnergy(acc)
	x := new(big.Int).Mul(meter.EnergyGrowthRate, vetBal)
	x.Mul(x, new(big.Int).SetUint64(time1-10))
	x.Div(x, big.NewInt(1e18))

	assert.Equal(t, x, bal1)
}
*/

func TestStorage(t *testing.T) {
	kv, _ := lvldb.NewMem()
	st, _ := New(meter.Bytes32{}, kv)

	addr := meter.BytesToAddress([]byte("addr"))
	key := meter.BytesToBytes32([]byte("key"))

	st.SetStorage(addr, key, meter.BytesToBytes32([]byte{1}))
	data, _ := rlp.EncodeToBytes([]byte{1})
	assert.Equal(t, rlp.RawValue(data), st.GetRawStorage(addr, key))

	st.SetRawStorage(addr, key, data)
	assert.Equal(t, meter.BytesToBytes32([]byte{1}), st.GetStorage(addr, key))

	st.SetStorage(addr, key, meter.Bytes32{})
	assert.Zero(t, len(st.GetRawStorage(addr, key)))

	v := struct {
		V1 uint
	}{313123}

	data, _ = rlp.EncodeToBytes(&v)
	st.SetRawStorage(addr, key, data)

	assert.Equal(t, meter.Blake2b(data), st.GetStorage(addr, key))
}

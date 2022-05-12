// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package builtin_test

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/meterio/meter-pov/abi"
	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/builtin"
	"github.com/meterio/meter-pov/chain"
	"github.com/meterio/meter-pov/genesis"
	"github.com/meterio/meter-pov/kv"
	"github.com/meterio/meter-pov/lvldb"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/runtime"
	"github.com/meterio/meter-pov/state"
	"github.com/meterio/meter-pov/tx"
	"github.com/meterio/meter-pov/xenv"
	"github.com/stretchr/testify/assert"
)

var errReverted = errors.New("evm: execution reverted")

type ctest struct {
	rt         *runtime.Runtime
	abi        *abi.ABI
	to, caller meter.Address
}

type ccase struct {
	rt         *runtime.Runtime
	abi        *abi.ABI
	to, caller meter.Address
	name       string
	args       []interface{}
	events     tx.Events
	provedWork *big.Int
	txID       meter.Bytes32
	blockRef   tx.BlockRef
	expiration uint32

	output *[]interface{}
	vmerr  error
}

func (c *ctest) Case(name string, args ...interface{}) *ccase {
	return &ccase{
		rt:     c.rt,
		abi:    c.abi,
		to:     c.to,
		caller: c.caller,
		name:   name,
		args:   args,
	}
}

func (c *ccase) To(to meter.Address) *ccase {
	c.to = to
	return c
}

func (c *ccase) Caller(caller meter.Address) *ccase {
	c.caller = caller
	return c
}

func (c *ccase) ProvedWork(provedWork *big.Int) *ccase {
	c.provedWork = provedWork
	return c
}

func (c *ccase) TxID(txID meter.Bytes32) *ccase {
	c.txID = txID
	return c
}

func (c *ccase) BlockRef(blockRef tx.BlockRef) *ccase {
	c.blockRef = blockRef
	return c
}

func (c *ccase) Expiration(expiration uint32) *ccase {
	c.expiration = expiration
	return c
}
func (c *ccase) ShouldVMError(err error) *ccase {
	c.vmerr = err
	return c
}

func (c *ccase) ShouldLog(events ...*tx.Event) *ccase {
	c.events = events
	return c
}

func (c *ccase) ShouldOutput(outputs ...interface{}) *ccase {
	c.output = &outputs
	return c
}

func (c *ccase) Assert(t *testing.T) *ccase {
	method, ok := c.abi.MethodByName(c.name)
	assert.True(t, ok, "should have method")

	constant := method.Const()
	stateRoot, err := c.rt.State().Stage().Hash()
	assert.Nil(t, err, "should hash state")

	data, err := method.EncodeInput(c.args...)
	assert.Nil(t, err, "should encode input")

	vmout := c.rt.ExecuteClause(tx.NewClause(&c.to).WithData(data),
		0, math.MaxUint64, &xenv.TransactionContext{
			ID:         c.txID,
			Origin:     c.caller,
			GasPrice:   &big.Int{},
			ProvedWork: c.provedWork,
			BlockRef:   c.blockRef,
			Expiration: c.expiration})

	if constant || vmout.VMErr != nil {
		newStateRoot, err := c.rt.State().Stage().Hash()
		assert.Nil(t, err, "should hash state")
		assert.Equal(t, stateRoot, newStateRoot)
	}
	if c.vmerr != nil {
		assert.Equal(t, c.vmerr, vmout.VMErr)
	} else {
		assert.Nil(t, vmout.VMErr)
	}

	if c.output != nil {
		out, err := method.EncodeOutput((*c.output)...)
		assert.Nil(t, err, "should encode output")
		assert.Equal(t, out, vmout.Data, "should match output")
	}

	if len(c.events) > 0 {
		for _, ev := range c.events {
			found := func() bool {
				for _, outEv := range vmout.Events {
					if reflect.DeepEqual(ev, outEv) {
						return true
					}
				}
				return false
			}()
			assert.True(t, found, "event should appear")
		}
	}

	assert.Nil(t, c.rt.State().Err(), "should no state error")

	c.output = nil
	c.vmerr = nil
	c.events = nil

	return c
}

func buildGenesis(kv kv.GetPutter, proc func(state *state.State) error) *block.Block {
	blk, _, _ := new(genesis.Builder).
		Timestamp(uint64(time.Now().Unix())).
		State(proc).
		Build(state.NewCreator(kv))
	return blk
}

func TestParamsNative(t *testing.T) {
	executor := meter.BytesToAddress([]byte("e"))
	kv, _ := lvldb.NewMem()
	b0 := buildGenesis(kv, func(state *state.State) error {
		state.SetCode(builtin.Params.Address, builtin.Params.RuntimeBytecodes())
		builtin.Params.Native(state).Set(meter.KeyExecutorAddress, new(big.Int).SetBytes(executor[:]))
		return nil
	})
	c, _ := chain.New(kv, b0, true)
	st, _ := state.New(b0.Header().StateRoot(), kv)
	seeker := c.NewSeeker(b0.Header().ID())
	defer func() {
		assert.Nil(t, st.Err())
		assert.Nil(t, seeker.Err())
	}()

	rt := runtime.New(seeker, st, &xenv.BlockContext{})

	test := &ctest{
		rt:  rt,
		abi: builtin.Params.ABI,
		to:  builtin.Params.Address,
	}

	key := meter.BytesToBytes32([]byte("key"))
	value := big.NewInt(999)
	setEvent := func(key meter.Bytes32, value *big.Int) *tx.Event {
		ev, _ := builtin.Params.ABI.EventByName("Set")
		data, _ := ev.Encode(value)
		return &tx.Event{
			Address: builtin.Params.Address,
			Topics:  []meter.Bytes32{ev.ID(), key},
			Data:    data,
		}
	}

	test.Case("executor").
		ShouldOutput(executor).
		Assert(t)

	test.Case("set", key, value).
		Caller(executor).
		ShouldLog(setEvent(key, value)).
		Assert(t)

	test.Case("set", key, value).
		ShouldVMError(errReverted).
		Assert(t)

	test.Case("get", key).
		ShouldOutput(value).
		Assert(t)

}

func TestPrototypeNative(t *testing.T) {
	var (
		acc1 = meter.BytesToAddress([]byte("acc1"))
		acc2 = meter.BytesToAddress([]byte("acc2"))

		master    = meter.BytesToAddress([]byte("master"))
		notmaster = meter.BytesToAddress([]byte("notmaster"))
		user      = meter.BytesToAddress([]byte("user"))
		notuser   = meter.BytesToAddress([]byte("notuser"))

		credit       = big.NewInt(1000)
		recoveryRate = big.NewInt(10)
		sponsor      = meter.BytesToAddress([]byte("sponsor"))
		notsponsor   = meter.BytesToAddress([]byte("notsponsor"))

		key      = meter.BytesToBytes32([]byte("account-key"))
		value    = meter.BytesToBytes32([]byte("account-value"))
		contract meter.Address
	)

	kv, _ := lvldb.NewMem()
	gene := genesis.NewDevnet()
	genesisBlock, _, _ := gene.Build(state.NewCreator(kv))
	c, _ := chain.New(kv, genesisBlock, true)
	st, _ := state.New(genesisBlock.Header().StateRoot(), kv)
	seeker := c.NewSeeker(genesisBlock.Header().ID())
	defer func() {
		assert.Nil(t, st.Err())
		assert.Nil(t, seeker.Err())
	}()

	st.SetStorage(meter.Address(acc1), key, value)
	st.SetBalance(meter.Address(acc1), big.NewInt(1))

	masterEvent := func(self, newMaster meter.Address) *tx.Event {
		ev, _ := builtin.Prototype.Events().EventByName("$Master")
		data, _ := ev.Encode(newMaster)
		return &tx.Event{
			Address: self,
			Topics:  []meter.Bytes32{ev.ID()},
			Data:    data,
		}
	}

	creditPlanEvent := func(self meter.Address, credit, recoveryRate *big.Int) *tx.Event {
		ev, _ := builtin.Prototype.Events().EventByName("$CreditPlan")
		data, _ := ev.Encode(credit, recoveryRate)
		return &tx.Event{
			Address: self,
			Topics:  []meter.Bytes32{ev.ID()},
			Data:    data,
		}
	}

	userEvent := func(self, user meter.Address, action string) *tx.Event {
		ev, _ := builtin.Prototype.Events().EventByName("$User")
		var b32 meter.Bytes32
		copy(b32[:], action)
		data, _ := ev.Encode(b32)
		return &tx.Event{
			Address: self,
			Topics:  []meter.Bytes32{ev.ID(), meter.BytesToBytes32(user.Bytes())},
			Data:    data,
		}
	}

	sponsorEvent := func(self, sponsor meter.Address, action string) *tx.Event {
		ev, _ := builtin.Prototype.Events().EventByName("$Sponsor")
		var b32 meter.Bytes32
		copy(b32[:], action)
		data, _ := ev.Encode(b32)
		return &tx.Event{
			Address: self,
			Topics:  []meter.Bytes32{ev.ID(), meter.BytesToBytes32(sponsor.Bytes())},
			Data:    data,
		}
	}

	rt := runtime.New(seeker, st, &xenv.BlockContext{
		Time:   genesisBlock.Header().Timestamp(),
		Number: genesisBlock.Header().Number(),
	})

	code, _ := hex.DecodeString("60606040523415600e57600080fd5b603580601b6000396000f3006060604052600080fd00a165627a7a72305820edd8a93b651b5aac38098767f0537d9b25433278c9d155da2135efc06927fc960029")
	out := rt.ExecuteClause(tx.NewClause(nil).WithData(code), 0, math.MaxUint64, &xenv.TransactionContext{
		ID:         meter.Bytes32{},
		Origin:     master,
		GasPrice:   &big.Int{},
		ProvedWork: &big.Int{}})
	contract = *out.ContractAddress

	energy := big.NewInt(1000)
	st.SetEnergy(acc1, energy)

	test := &ctest{
		rt:     rt,
		abi:    builtin.Prototype.ABI,
		to:     builtin.Prototype.Address,
		caller: builtin.Prototype.Address,
	}

	test.Case("master", acc1).
		ShouldOutput(meter.Address{}).
		Assert(t)

	test.Case("master", contract).
		ShouldOutput(master).
		Assert(t)

	test.Case("setMaster", acc1, acc2).
		Caller(acc1).
		ShouldOutput().
		ShouldLog(masterEvent(acc1, acc2)).
		Assert(t)

	test.Case("setMaster", acc1, acc2).
		Caller(notmaster).
		ShouldVMError(errReverted).
		Assert(t)

	test.Case("master", acc1).
		ShouldOutput(acc2).
		Assert(t)

	test.Case("hasCode", acc1).
		ShouldOutput(false).
		Assert(t)

	test.Case("hasCode", contract).
		ShouldOutput(true).
		Assert(t)

	test.Case("setCreditPlan", contract, credit, recoveryRate).
		Caller(master).
		ShouldOutput().
		ShouldLog(creditPlanEvent(contract, credit, recoveryRate)).
		Assert(t)

	test.Case("setCreditPlan", contract, credit, recoveryRate).
		Caller(notmaster).
		ShouldVMError(errReverted).
		Assert(t)

	test.Case("creditPlan", contract).
		ShouldOutput(credit, recoveryRate).
		Assert(t)

	test.Case("isUser", contract, user).
		ShouldOutput(false).
		Assert(t)

	test.Case("addUser", contract, user).
		Caller(master).
		ShouldOutput().
		ShouldLog(userEvent(contract, user, "added")).
		Assert(t)

	test.Case("addUser", contract, user).
		Caller(notmaster).
		ShouldVMError(errReverted).
		Assert(t)

	test.Case("addUser", contract, user).
		Caller(master).
		ShouldVMError(errReverted).
		Assert(t)

	test.Case("isUser", contract, user).
		ShouldOutput(true).
		Assert(t)

	test.Case("userCredit", contract, user).
		ShouldOutput(credit).
		Assert(t)

	test.Case("removeUser", contract, user).
		Caller(master).
		ShouldOutput().
		ShouldLog(userEvent(contract, user, "removed")).
		Assert(t)

	test.Case("removeUser", contract, user).
		Caller(notmaster).
		ShouldVMError(errReverted).
		Assert(t)

	test.Case("removeUser", contract, notuser).
		Caller(master).
		ShouldVMError(errReverted).
		Assert(t)

	test.Case("userCredit", contract, user).
		ShouldOutput(&big.Int{}).
		Assert(t)

	test.Case("isSponsor", contract, sponsor).
		ShouldOutput(false).
		Assert(t)

	test.Case("sponsor", contract).
		Caller(sponsor).
		ShouldOutput().
		ShouldLog(sponsorEvent(contract, sponsor, "sponsored")).
		Assert(t)

	test.Case("sponsor", contract).
		Caller(sponsor).
		ShouldVMError(errReverted).
		Assert(t)

	test.Case("isSponsor", contract, sponsor).
		ShouldOutput(true).
		Assert(t)

	test.Case("currentSponsor", contract).
		ShouldOutput(meter.Address{}).
		Assert(t)

	test.Case("selectSponsor", contract, sponsor).
		Caller(master).
		ShouldOutput().
		ShouldLog(sponsorEvent(contract, sponsor, "selected")).
		Assert(t)

	test.Case("selectSponsor", contract, notsponsor).
		Caller(master).
		ShouldVMError(errReverted).
		Assert(t)

	test.Case("selectSponsor", contract, notsponsor).
		Caller(notmaster).
		ShouldVMError(errReverted).
		Assert(t)
	test.Case("currentSponsor", contract).
		ShouldOutput(sponsor).
		Assert(t)

	test.Case("unsponsor", contract).
		Caller(sponsor).
		ShouldOutput().
		Assert(t)
	test.Case("currentSponsor", contract).
		ShouldOutput(sponsor).
		Assert(t)

	test.Case("unsponsor", contract).
		Caller(sponsor).
		ShouldVMError(errReverted).
		Assert(t)

	test.Case("isSponsor", contract, sponsor).
		ShouldOutput(false).
		Assert(t)

	test.Case("storageFor", acc1, key).
		ShouldOutput(value).
		Assert(t)
	test.Case("storageFor", acc1, meter.BytesToBytes32([]byte("some-key"))).
		ShouldOutput(meter.Bytes32{}).
		Assert(t)

	// should be hash of rlp raw
	test.Case("storageFor", builtin.Prototype.Address, meter.Blake2b(contract.Bytes(), []byte("credit-plan"))).
		ShouldOutput(st.GetStorage(builtin.Prototype.Address, meter.Blake2b(contract.Bytes(), []byte("credit-plan")))).
		Assert(t)

	test.Case("balance", acc1, big.NewInt(0)).
		ShouldOutput(big.NewInt(1)).
		Assert(t)

	test.Case("balance", acc1, big.NewInt(100)).
		ShouldOutput(big.NewInt(0)).
		Assert(t)

	test.Case("energy", acc1, big.NewInt(0)).
		ShouldOutput(energy).
		Assert(t)

	test.Case("energy", acc1, big.NewInt(100)).
		ShouldOutput(big.NewInt(0)).
		Assert(t)

	assert.False(t, st.GetCodeHash(builtin.Prototype.Address).IsZero())

}

func TestPrototypeNativeWithLongerBlockNumber(t *testing.T) {
	var (
		acc1 = meter.BytesToAddress([]byte("acc1"))
	)

	kv, _ := lvldb.NewMem()
	gene := genesis.NewDevnet()
	genesisBlock, _, _ := gene.Build(state.NewCreator(kv))
	st, _ := state.New(genesisBlock.Header().StateRoot(), kv)
	c, _ := chain.New(kv, genesisBlock, true)
	launchTime := genesisBlock.Header().Timestamp()

	for i := 1; i < 100; i++ {
		st.SetBalance(acc1, big.NewInt(int64(i)))
		st.SetEnergy(acc1, big.NewInt(int64(i)))
		stateRoot, _ := st.Stage().Commit()
		b := new(block.Builder).
			ParentID(c.BestBlock().Header().ID()).
			TotalScore(c.BestBlock().Header().TotalScore() + 1).
			Timestamp(launchTime + uint64(i)*10).
			StateRoot(stateRoot).
			Build()
		qc := block.QuorumCert{QCHeight: uint32(i), QCRound: uint32(i), EpochID: uint64(0)}
		b.SetQC(&qc)
		fmt.Println("BLOKC:", b)
		c.AddBlock(b, tx.Receipts{}, true)
	}

	st, _ = state.New(c.BestBlock().Header().StateRoot(), kv)
	seeker := c.NewSeeker(c.BestBlock().Header().ID())
	defer func() {
		assert.Nil(t, st.Err())
		assert.Nil(t, seeker.Err())
	}()
	rt := runtime.New(seeker, st, &xenv.BlockContext{
		Number: meter.MaxBackTrackingBlockNumber + 1,
		Time:   c.BestBlock().Header().Timestamp(),
	})

	test := &ctest{
		rt:     rt,
		abi:    builtin.Prototype.ABI,
		to:     builtin.Prototype.Address,
		caller: builtin.Prototype.Address,
	}

	test.Case("balance", acc1, big.NewInt(0)).
		ShouldOutput(big.NewInt(0)).
		Assert(t)

	test.Case("energy", acc1, big.NewInt(0)).
		ShouldOutput(big.NewInt(0)).
		Assert(t)

	test.Case("balance", acc1, big.NewInt(1)).
		ShouldOutput(big.NewInt(1)).
		Assert(t)

	test.Case("energy", acc1, big.NewInt(1)).
		ShouldOutput(big.NewInt(1)).
		Assert(t)

	test.Case("balance", acc1, big.NewInt(2)).
		ShouldOutput(big.NewInt(2)).
		Assert(t)

	test.Case("energy", acc1, big.NewInt(2)).
		ShouldOutput(big.NewInt(2)).
		Assert(t)
}

func TestPrototypeNativeWithBlockNumber(t *testing.T) {
	var (
		acc1 = meter.BytesToAddress([]byte("acc1"))
	)

	kv, _ := lvldb.NewMem()
	gene := genesis.NewDevnet()
	genesisBlock, _, _ := gene.Build(state.NewCreator(kv))
	st, _ := state.New(genesisBlock.Header().StateRoot(), kv)
	c, _ := chain.New(kv, genesisBlock, true)
	launchTime := genesisBlock.Header().Timestamp()

	for i := 1; i < 100; i++ {
		st.SetBalance(acc1, big.NewInt(int64(i)))
		st.SetEnergy(acc1, big.NewInt(int64(i)))
		stateRoot, _ := st.Stage().Commit()
		b := new(block.Builder).
			ParentID(c.BestBlock().Header().ID()).
			TotalScore(c.BestBlock().Header().TotalScore() + 1).
			Timestamp(launchTime + uint64(i)*10).
			StateRoot(stateRoot).
			Build()
		qc := block.QuorumCert{QCHeight: uint32(i), QCRound: uint32(i), EpochID: uint64(0)}
		b.SetQC(&qc)
		c.AddBlock(b, tx.Receipts{}, true)
	}

	st, _ = state.New(c.BestBlock().Header().StateRoot(), kv)
	seeker := c.NewSeeker(c.BestBlock().Header().ID())
	defer func() {
		assert.Nil(t, st.Err())
		assert.Nil(t, seeker.Err())
	}()
	rt := runtime.New(seeker, st, &xenv.BlockContext{
		Number: c.BestBlock().Header().Number(),
		Time:   c.BestBlock().Header().Timestamp(),
	})

	test := &ctest{
		rt:     rt,
		abi:    builtin.Prototype.ABI,
		to:     builtin.Prototype.Address,
		caller: builtin.Prototype.Address,
	}

	test.Case("balance", acc1, big.NewInt(10)).
		ShouldOutput(big.NewInt(10)).
		Assert(t)

	test.Case("energy", acc1, big.NewInt(10)).
		ShouldOutput(big.NewInt(10)).
		Assert(t)

	test.Case("balance", acc1, big.NewInt(99)).
		ShouldOutput(big.NewInt(99)).
		Assert(t)

	test.Case("energy", acc1, big.NewInt(99)).
		ShouldOutput(big.NewInt(99)).
		Assert(t)
}

func newBlock(parent *block.Block, score uint64, timestamp uint64, privateKey *ecdsa.PrivateKey) *block.Block {
	b := new(block.Builder).ParentID(parent.Header().ID()).TotalScore(parent.Header().TotalScore() + score).Timestamp(timestamp).Build()
	qc := block.QuorumCert{QCHeight: parent.QC.QCHeight + 1, QCRound: parent.QC.QCRound + 1}
	b.SetQC(&qc)
	sig, _ := crypto.Sign(b.Header().SigningHash().Bytes(), privateKey)
	return b.WithSignature(sig)
}

/*
func TestExtensionNative(t *testing.T) {
	kv, _ := lvldb.NewMem()
	st, _ := state.New(meter.Bytes32{}, kv)
	gene := genesis.NewDevnet()
	genesisBlock, _, _ := gene.Build(state.NewCreator(kv))
	c, _ := chain.New(kv, genesisBlock, true)
	st.SetCode(builtin.Extension.Address, builtin.Extension.RuntimeBytecodes())

	privKeys := make([]*ecdsa.PrivateKey, 2)

	for i := 0; i < 2; i++ {
		privateKey, _ := crypto.GenerateKey()
		privKeys[i] = privateKey
	}

	b0 := genesisBlock
	b1 := newBlock(b0, 123, 456, privKeys[0])
	b2 := newBlock(b1, 789, 321, privKeys[1])

	b1_singer, _ := b1.Header().Signer()
	b2_singer, _ := b2.Header().Signer()

	_, err := c.AddBlock(b1, nil, true)
	assert.Equal(t, err, nil)
	_, err = c.AddBlock(b2, nil, true)
	assert.Equal(t, err, nil)

	seeker := c.NewSeeker(b2.Header().ID())
	defer func() {
		assert.Nil(t, st.Err())
		assert.Nil(t, seeker.Err())
	}()
	rt := runtime.New(seeker, st, &xenv.BlockContext{Number: 2, Time: b2.Header().Timestamp(), TotalScore: b2.Header().TotalScore(), Signer: b2_singer})

	test := &ctest{
		rt:  rt,
		abi: builtin.Extension.ABI,
		to:  builtin.Extension.Address,
	}

	test.Case("blake2b256", []byte("hello world")).
		ShouldOutput(meter.Blake2b([]byte("hello world"))).
		Assert(t)

	// test.Case("totalSupply").
	// 	ShouldOutput(builtin.Energy.Native(st, 0).TokenTotalSupply()).
	// 	Assert(t)

	test.Case("txBlockRef").
		BlockRef(tx.NewBlockRef(1)).
		ShouldOutput(tx.NewBlockRef(1)).
		Assert(t)

	test.Case("txExpiration").
		Expiration(100).
		ShouldOutput(big.NewInt(100)).
		Assert(t)

	test.Case("txProvedWork").
		ProvedWork(big.NewInt(1e12)).
		ShouldOutput(big.NewInt(1e12)).
		Assert(t)

	test.Case("txID").
		TxID(meter.BytesToBytes32([]byte("txID"))).
		ShouldOutput(meter.BytesToBytes32([]byte("txID"))).
		Assert(t)

	test.Case("blockID", big.NewInt(3)).
		ShouldOutput(meter.Bytes32{}).
		Assert(t)

	test.Case("blockID", big.NewInt(2)).
		ShouldOutput(meter.Bytes32{}).
		Assert(t)

	test.Case("blockID", big.NewInt(1)).
		ShouldOutput(b1.Header().ID()).
		Assert(t)

	test.Case("blockID", big.NewInt(0)).
		ShouldOutput(b0.Header().ID()).
		Assert(t)

	test.Case("blockTotalScore", big.NewInt(3)).
		ShouldOutput(uint64(0)).
		Assert(t)

	test.Case("blockTotalScore", big.NewInt(2)).
		ShouldOutput(b2.Header().TotalScore()).
		Assert(t)

	test.Case("blockTotalScore", big.NewInt(1)).
		ShouldOutput(b1.Header().TotalScore()).
		Assert(t)

	test.Case("blockTotalScore", big.NewInt(0)).
		ShouldOutput(b0.Header().TotalScore()).
		Assert(t)

	test.Case("blockTime", big.NewInt(3)).
		ShouldOutput(&big.Int{}).
		Assert(t)

	test.Case("blockTime", big.NewInt(2)).
		ShouldOutput(new(big.Int).SetUint64(b2.Header().Timestamp())).
		Assert(t)

	test.Case("blockTime", big.NewInt(1)).
		ShouldOutput(new(big.Int).SetUint64(b1.Header().Timestamp())).
		Assert(t)

	test.Case("blockTime", big.NewInt(0)).
		ShouldOutput(new(big.Int).SetUint64(b0.Header().Timestamp())).
		Assert(t)

	test.Case("blockSigner", big.NewInt(3)).
		ShouldOutput(meter.Address{}).
		Assert(t)

	test.Case("blockSigner", big.NewInt(2)).
		ShouldOutput(b2_singer).
		Assert(t)

	test.Case("blockSigner", big.NewInt(1)).
		ShouldOutput(b1_singer).
		Assert(t)
}
*/

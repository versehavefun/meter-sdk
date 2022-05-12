// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	. "github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/tx"

	// "crypto/rand"
	// cmn "github.com/meterio/meter-pov/libs/common"

	"github.com/meterio/meter-pov/types"
)

func TestSerialize(t *testing.T) {

	tx1 := new(tx.Builder).Clause(tx.NewClause(&meter.Address{})).Clause(tx.NewClause(&meter.Address{})).Build()
	tx2 := new(tx.Builder).Clause(tx.NewClause(nil)).Build()

	privKey := string("dce1443bd2ef0c2631adc1c67e5c93f13dc23a41c18b536effbbdcbcdb96fb65")

	now := uint64(time.Now().UnixNano())

	var (
		gasUsed     uint64        = 1000
		gasLimit    uint64        = 14000
		totalScore  uint64        = 101
		emptyRoot   meter.Bytes32 = meter.BytesToBytes32([]byte("0"))
		beneficiary meter.Address = meter.BytesToAddress([]byte("abc"))
	)

	block := new(Builder).
		GasUsed(gasUsed).
		Transaction(tx1).
		Transaction(tx2).
		GasLimit(gasLimit).
		TotalScore(totalScore).
		StateRoot(emptyRoot).
		ReceiptsRoot(emptyRoot).
		Timestamp(now).
		ParentID(emptyRoot).
		Beneficiary(beneficiary).
		Build()

	h := block.Header()

	txs := block.Transactions()
	body := block.Body()
	txsRootHash := txs.RootHash()

	fmt.Println(h.ID())

	assert.Equal(t, body.Txs, txs)
	assert.Equal(t, Compose(h, txs), block)
	assert.Equal(t, gasLimit, h.GasLimit())
	assert.Equal(t, gasUsed, h.GasUsed())
	assert.Equal(t, totalScore, h.TotalScore())
	assert.Equal(t, emptyRoot, h.StateRoot())
	assert.Equal(t, emptyRoot, h.ReceiptsRoot())
	assert.Equal(t, now, h.Timestamp())
	assert.Equal(t, emptyRoot, h.ParentID())
	assert.Equal(t, beneficiary, h.Beneficiary())
	assert.Equal(t, txsRootHash, h.TxsRoot())

	key, _ := crypto.HexToECDSA(privKey)
	sig, _ := crypto.Sign(block.Header().SigningHash().Bytes(), key)

	block = block.WithSignature(sig)
	kBlockData := KBlockData{Nonce: 1111}
	block.SetKBlockData(kBlockData)

	qc := QuorumCert{QCHeight: 1, QCRound: 1, EpochID: 1, VoterAggSig: []byte{1, 2, 3}, VoterBitArrayStr: "**-", VoterMsgHash: [32]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, VoterViolation: []*Violation{}}
	block.SetQC(&qc)

	addr := types.NetAddress{IP: []byte{}, Port: 4444}
	committeeInfo := []CommitteeInfo{CommitteeInfo{
		Name:     "Testee",
		CSIndex:  0,
		CSPubKey: []byte{},
		PubKey:   []byte{},
		NetAddr:  addr,
	}}
	addrBytes, err := rlp.EncodeToBytes(addr)
	fmt.Println(addrBytes, err)
	_, err = rlp.EncodeToBytes(committeeInfo)
	assert.Equal(t, err, nil)

	block.SetCommitteeInfo(committeeInfo)

	fmt.Println("BEFORE KBlockData data:", kBlockData)
	fmt.Println("BEFORE block.KBlockData:", block.KBlockData)
	fmt.Println("BEFORE block.CommitteeInfo: ", committeeInfo)
	fmt.Println("BEFORE block.CommitteeInfo: ", block.CommitteeInfos)
	fmt.Println("BEFORE BLOCK:", block)
	data, err := rlp.EncodeToBytes(block)
	assert.Equal(t, err, nil)
	fmt.Println("BLOCK SERIALIZED TO:", data)

	b := &Block{}

	err = rlp.DecodeBytes(data, b)
	assert.Equal(t, err, nil)
	fmt.Println("AFTER BLOCK:", b)
	kb, err := b.GetKBlockData()
	assert.Equal(t, err, nil)
	assert.Equal(t, kBlockData.Nonce, kb.Nonce)

	ci, err := b.GetCommitteeInfo()
	assert.Equal(t, err, nil)
	assert.Equal(t, len(committeeInfo), len(ci))
	assert.Equal(t, committeeInfo[0].Name, ci[0].Name)

	dqc := b.GetQC()
	assert.Equal(t, qc.QCHeight, dqc.QCHeight)
}

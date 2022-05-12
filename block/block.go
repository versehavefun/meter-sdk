// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/rlp"
	cmn "github.com/meterio/meter-pov/libs/common"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/metric"
	"github.com/meterio/meter-pov/tx"
	"github.com/meterio/meter-pov/types"
)

const (
	DoubleSign = int(1)
)

var (
	BlockMagicVersion1 [4]byte = [4]byte{0x76, 0x01, 0x00, 0x00} // version v.1.0.0
)

type Violation struct {
	Type       int
	Index      int
	Address    meter.Address
	MsgHash    [32]byte
	Signature1 []byte
	Signature2 []byte
}

// NewEvidence records the voting/notarization aggregated signatures and bitmap
// of validators.
// Validators info can get from 1st proposaed block meta data
type Evidence struct {
	VotingSig       []byte //serialized bls signature
	VotingMsgHash   []byte //[][32]byte
	VotingBitArray  cmn.BitArray
	VotingViolation []*Violation

	NotarizeSig       []byte
	NotarizeMsgHash   []byte //[][32]byte
	NotarizeBitArray  cmn.BitArray
	NotarizeViolation []*Violation
}

type PowRawBlock []byte

type KBlockData struct {
	Nonce uint64 // the last of the pow block
	Data  []PowRawBlock
	Proof []byte
}

func (d KBlockData) ToString() string {
	hexs := make([]string, 0)
	for _, r := range d.Data {
		hexs = append(hexs, hex.EncodeToString(r))
	}
	return fmt.Sprintf("KBlockData(Nonce:%v, Proof:%v, Data:%v)", d.Nonce, d.Proof, strings.Join(hexs, ","))
}

type CommitteeInfo struct {
	Name     string
	CSIndex  uint32 // Index, corresponding to the bitarray
	NetAddr  types.NetAddress
	CSPubKey []byte // Bls pubkey
	PubKey   []byte // ecdsa pubkey
}

func (ci CommitteeInfo) String() string {
	ecdsaPK := base64.StdEncoding.EncodeToString(ci.PubKey)
	blsPK := base64.StdEncoding.EncodeToString(ci.CSPubKey)
	return fmt.Sprintf("%v: { Name:%v, IP:%v, ECDSA_PK:%v, BLS_PK:%v }", ci.CSIndex, ci.Name, ci.NetAddr.IP.String(), ecdsaPK, blsPK)
}

type CommitteeInfos struct {
	Epoch         uint64
	CommitteeInfo []CommitteeInfo
}

func (cis CommitteeInfos) String() string {
	s := make([]string, 0)
	for _, ci := range cis.CommitteeInfo {
		s = append(s, ci.String())
	}
	if len(s) == 0 {
		return "CommitteeInfos(nil)"
	}
	return "CommitteeInfos(\n  " + strings.Join(s, ",\n  ") + "\n)"
}

// Block is an immutable block type.
type Block struct {
	BlockHeader    *Header
	Txs            tx.Transactions
	QC             *QuorumCert
	CommitteeInfos CommitteeInfos
	KBlockData     KBlockData
	Magic          [4]byte
	cache          struct {
		size atomic.Value
	}
}

// Body defines body of a block.
type Body struct {
	Txs tx.Transactions
}

// Create new Evidence
func NewEvidence(votingSig []byte, votingMsgHash [][32]byte, votingBA cmn.BitArray,
	notarizeSig []byte, notarizeMsgHash [][32]byte, notarizeBA cmn.BitArray) *Evidence {
	return &Evidence{
		VotingSig:        votingSig,
		VotingMsgHash:    cmn.Byte32ToByteSlice(votingMsgHash),
		VotingBitArray:   votingBA,
		NotarizeSig:      notarizeSig,
		NotarizeMsgHash:  cmn.Byte32ToByteSlice(notarizeMsgHash),
		NotarizeBitArray: notarizeBA,
	}
}

// Create new committee Info
func NewCommitteeInfo(name string, pubKey []byte, netAddr types.NetAddress, csPubKey []byte, csIndex uint32) *CommitteeInfo {
	return &CommitteeInfo{
		Name:     name,
		PubKey:   pubKey,
		NetAddr:  netAddr,
		CSPubKey: csPubKey,
		CSIndex:  csIndex,
	}
}

// Compose compose a block with all needed components
// Note: This method is usually to recover a block by its portions, and the TxsRoot is not verified.
// To build up a block, use a Builder.
func Compose(header *Header, txs tx.Transactions) *Block {
	return &Block{
		BlockHeader: header,
		Txs:         append(tx.Transactions(nil), txs...),
	}
}

// WithSignature create a new block object with signature set.
func (b *Block) WithSignature(sig []byte) *Block {
	return &Block{
		BlockHeader: b.BlockHeader.withSignature(sig),
		Txs:         b.Txs,
	}
}

// Header returns the block header.
func (b *Block) Header() *Header {
	return b.BlockHeader
}

func (b *Block) ID() meter.Bytes32 {
	return b.BlockHeader.ID()
}

// ParentID returns id of parent block.
func (b *Block) ParentID() meter.Bytes32 {
	return b.BlockHeader.ParentID()
}

// LastBlocID returns id of parent block.
func (b *Block) LastKBlockHeight() uint32 {
	return b.BlockHeader.LastKBlockHeight()
}

// Number returns sequential number of this block.
func (b *Block) Number() uint32 {
	// inferred from parent id
	return b.BlockHeader.Number()
}

// Timestamp returns timestamp of this block.
func (b *Block) Timestamp() uint64 {
	return b.BlockHeader.Timestamp()
}

// BlockType returns block type of this block.
func (b *Block) BlockType() uint32 {
	return b.BlockHeader.BlockType()
}

func (b *Block) IsKBlock() bool {
	return b.BlockHeader.BlockType() == BLOCK_TYPE_K_BLOCK
}

func (b *Block) IsSBlock() bool {
	return b.BlockHeader.BlockType() == BLOCK_TYPE_S_BLOCK
}

// TotalScore returns total score that cumulated from genesis block to this one.
func (b *Block) TotalScore() uint64 {
	return b.BlockHeader.TotalScore()
}

// GasLimit returns gas limit of this block.
func (b *Block) GasLimit() uint64 {
	return b.BlockHeader.GasLimit()
}

// GasUsed returns gas used by txs.
func (b *Block) GasUsed() uint64 {
	return b.BlockHeader.GasUsed()
}

// Beneficiary returns reward recipient.
func (b *Block) Beneficiary() meter.Address {
	return b.BlockHeader.Beneficiary()
}

// TxsRoot returns merkle root of txs contained in this block.
func (b *Block) TxsRoot() meter.Bytes32 {
	return b.BlockHeader.TxsRoot()
}

// StateRoot returns account state merkle root just afert this block being applied.
func (b *Block) StateRoot() meter.Bytes32 {
	return b.BlockHeader.StateRoot()
}

// ReceiptsRoot returns merkle root of tx receipts.
func (b *Block) ReceiptsRoot() meter.Bytes32 {
	return b.BlockHeader.ReceiptsRoot()
}

// EvidenceDataRoot returns merkle root of tx receipts.
func (b *Block) EvidenceDataRoot() meter.Bytes32 {
	return b.BlockHeader.EvidenceDataRoot()
}

func (b *Block) Signer() (signer meter.Address, err error) {
	return b.BlockHeader.Signer()
}

// Transactions returns a copy of transactions.
func (b *Block) Transactions() tx.Transactions {
	return append(tx.Transactions(nil), b.Txs...)
}

// Body returns body of a block.
func (b *Block) Body() *Body {
	return &Body{append(tx.Transactions(nil), b.Txs...)}
}

// EncodeRLP implements rlp.Encoder.
func (b *Block) EncodeRLP(w io.Writer) error {
	if b == nil {
		w.Write([]byte{})
		return nil
	}
	return rlp.Encode(w, []interface{}{
		b.BlockHeader,
		b.Txs,
		b.KBlockData,
		b.CommitteeInfos,
		b.QC,
		b.Magic,
	})
}

// DecodeRLP implements rlp.Decoder.
func (b *Block) DecodeRLP(s *rlp.Stream) error {
	_, size, err := s.Kind()
	if err != nil {
		fmt.Println("decode rlp error:", err)
	}

	payload := struct {
		Header         Header
		Txs            tx.Transactions
		KBlockData     KBlockData
		CommitteeInfos CommitteeInfos
		QC             *QuorumCert
		Magic          [4]byte
	}{}

	if err := s.Decode(&payload); err != nil {
		return err
	}

	*b = Block{
		BlockHeader:    &payload.Header,
		Txs:            payload.Txs,
		KBlockData:     payload.KBlockData,
		CommitteeInfos: payload.CommitteeInfos,
		QC:             payload.QC,
		Magic:          payload.Magic,
	}
	b.cache.size.Store(metric.StorageSize(rlp.ListSize(size)))
	return nil
}

// Size returns block size in bytes.
func (b *Block) Size() metric.StorageSize {
	if cached := b.cache.size.Load(); cached != nil {
		return cached.(metric.StorageSize)
	}
	var size metric.StorageSize
	err := rlp.Encode(&size, b)
	if err != nil {
		fmt.Println("block size error:", err)
	}

	b.cache.size.Store(size)
	return size
}

func (b *Block) String() string {
	canonicalName := b.GetCanonicalName()
	return fmt.Sprintf(`%v(%v){
BlockHeader: %v,
Magic: %v,
Transactions: %v,
KBlockData: %v,
CommitteeInfo: %v,
QuorumCert: %v,
}`, canonicalName, b.BlockHeader.Number(), b.BlockHeader, "0x"+hex.EncodeToString(b.Magic[:]), b.Txs, b.KBlockData.ToString(), b.CommitteeInfos, b.QC)
}

func (b *Block) CompactString() string {
	header := b.BlockHeader
	hasCommittee := len(b.CommitteeInfos.CommitteeInfo) > 0
	ci := "no"
	if hasCommittee {
		ci = "YES"
	}
	return fmt.Sprintf(`%v(%v) %v 
  Parent: %v,
  QC: %v,
  LastKBHeight: %v, Magic: %v, #Txs: %v, CommitteeInfo: %v`, b.GetCanonicalName(), header.Number(), header.ID().String(),
		header.ParentID().String(),
		b.QC.CompactString(),
		header.LastKBlockHeight(), b.Magic, len(b.Txs), ci)
}

func (b *Block) GetCanonicalName() string {
	if b == nil {
		return ""
	}
	switch b.BlockHeader.BlockType() {
	case BLOCK_TYPE_K_BLOCK:
		return "kBlock"
	case BLOCK_TYPE_M_BLOCK:
		return "mBlock"
	case BLOCK_TYPE_S_BLOCK:
		return "sBlock"
	default:
		return "Block"
	}
}
func (b *Block) Oneliner() string {
	header := b.BlockHeader
	hasCommittee := len(b.CommitteeInfos.CommitteeInfo) > 0
	ci := "no"
	if hasCommittee {
		ci = "YES"
	}
	canonicalName := b.GetCanonicalName()
	return fmt.Sprintf("%v(%v) %v QC:%v, Magic:%v, #Txs:%v, CI:%v, Parent:%v ", canonicalName,
		header.Number(), header.ID().String(), b.QC.CompactString(), b.Magic, len(b.Transactions()), ci, header.ParentID())
}

//-----------------
func (b *Block) SetMagic(m [4]byte) *Block {
	b.Magic = m
	return b
}
func (b *Block) GetMagic() [4]byte {
	return b.Magic
}

func (b *Block) SetQC(qc *QuorumCert) *Block {
	b.QC = qc
	return b
}
func (b *Block) GetQC() *QuorumCert {
	return b.QC
}

// Serialization for KBlockData and ComitteeInfo
func (b *Block) GetKBlockData() (*KBlockData, error) {
	return &b.KBlockData, nil
}

func (b *Block) SetKBlockData(data KBlockData) error {
	b.KBlockData = data
	return nil
}

func (b *Block) GetCommitteeEpoch() uint64 {
	return b.CommitteeInfos.Epoch
}

func (b *Block) SetCommitteeEpoch(epoch uint64) {
	b.CommitteeInfos.Epoch = epoch
}

func (b *Block) GetCommitteeInfo() ([]CommitteeInfo, error) {
	return b.CommitteeInfos.CommitteeInfo, nil
}

// if the block is the first mblock, get epochID from committee
// otherwise get epochID from QC
func (b *Block) GetBlockEpoch() (epoch uint64) {
	height := b.Header().Number()
	lastKBlockHeight := b.Header().LastKBlockHeight()
	if height == 0 {
		epoch = 0
		return
	}

	if height > lastKBlockHeight+1 {
		epoch = b.QC.EpochID
	} else if height == lastKBlockHeight+1 {
		epoch = b.GetCommitteeEpoch()
	} else {
		panic("Block error: lastKBlockHeight great than height")
	}
	return
}

func (b *Block) SetCommitteeInfo(info []CommitteeInfo) {
	b.CommitteeInfos.CommitteeInfo = info
}

func (b *Block) ToBytes() []byte {
	bytes, err := rlp.EncodeToBytes(b)
	if err != nil {
		fmt.Println("tobytes error:", err)
	}

	return bytes
}

func (b *Block) EvidenceDataHash() (hash meter.Bytes32) {
	hw := meter.NewBlake2b()
	err := rlp.Encode(hw, []interface{}{
		b.QC.QCHeight,
		b.QC.QCRound,
		// b.QC.VotingBitArray,
		b.QC.VoterMsgHash,
		b.QC.VoterAggSig,
		b.CommitteeInfos,
		b.KBlockData,
	})
	if err != nil {
		fmt.Println("error:", err)
	}

	hw.Sum(hash[:0])
	return
}

func (b *Block) SetEvidenceDataHash(hash meter.Bytes32) error {
	b.BlockHeader.Body.EvidenceDataRoot = hash
	return nil
}

func (b *Block) SetBlockSignature(sig []byte) error {
	cpy := append([]byte(nil), sig...)
	b.BlockHeader.Body.Signature = cpy
	return nil
}

//--------------
func BlockEncodeBytes(blk *Block) []byte {
	blockBytes, err := rlp.EncodeToBytes(blk)
	if err != nil {
		fmt.Println("block encode error: ", err)
		return make([]byte, 0)
	}

	return blockBytes
}

func BlockDecodeFromBytes(bytes []byte) (*Block, error) {
	blk := Block{}
	err := rlp.DecodeBytes(bytes, &blk)
	//fmt.Println("decode failed", err)
	return &blk, err
}

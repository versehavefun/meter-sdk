package consensus

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sync"

	//"errors"
	b64 "encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/cors"
	cli "gopkg.in/urfave/cli.v1"

	"github.com/dfinlab/meter/block"
	"github.com/dfinlab/meter/chain"
	"github.com/dfinlab/meter/comm"
	bls "github.com/dfinlab/meter/crypto/multi_sig"
	"github.com/dfinlab/meter/genesis"
	cmn "github.com/dfinlab/meter/libs/common"
	"github.com/dfinlab/meter/meter"
	"github.com/dfinlab/meter/powpool"
	"github.com/dfinlab/meter/script/staking"
	"github.com/dfinlab/meter/state"
	"github.com/dfinlab/meter/types"
	crypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/inconshreveable/log15"
)

const (
	//maxMsgSize = 1048576 // 1MB;
	// set as 1184 * 1024
	maxMsgSize = 1300000 // gasLimit 20000000 generate, 1024+1024 (1048576) + sizeof(QC) + sizeof(committee)...

	//normally when a block is committed, wait for a while to let whole network to sync and move to next round
	WHOLE_NETWORK_BLOCK_SYNC_TIME = 5 * time.Second

	blocksToContributeToBecomeGoodPeer = 10000
	votesToContributeToBecomeGoodPeer  = 10000

	COMMITTEE_SIZE = 400  // by default
	DELEGATES_SIZE = 2000 // by default

	// Sign Announce Mesage
	// "Announce Committee Message: Leader <pubkey 64(hexdump 32x2) bytes> EpochID <8 (4x2)bytes> Height <16 (8x2) bytes> Round <8(4x2)bytes>
	ANNOUNCE_SIGN_MSG_SIZE = int(110)

	// Sign Propopal Message
	// "Proposal Block Message: Proposer <pubkey 64(32x3)> BlockType <2 bytes> Height <16 (8x2) bytes> Round <8 (4x2) bytes>
	PROPOSAL_SIGN_MSG_SIZE = int(100)

	// Sign Notary Announce Message
	// "Announce Notarization Message: Leader <pubkey 64(32x3)> EpochID <8 bytes> Height <16 (8x2) bytes> Round <8 (4x2) bytes>
	NOTARY_ANNOUNCE_SIGN_MSG_SIZE = int(120)

	// Sign Notary Block Message
	// "Block Notarization Message: Proposer <pubkey 64(32x3)> BlockType <8 bytes> Height <16 (8x2) bytes> Round <8 (4x2) bytes>
	NOTARY_BLOCK_SIGN_MSG_SIZE = int(130)

	CHAN_DEFAULT_BUF_SIZE = 100

	MAX_PEERS = 8

	DEFAULT_EPOCHS_PERDAY = 24
)

var (
	ConsensusGlobInst *ConsensusReactor

	curRoundGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "current_round",
		Help: "Current round of consensus",
	})
	inCommitteeGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "in_committee",
		Help: "is this node in committee",
	})
	pmRoleGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pacemaker_role",
		Help: "Role in pacemaker",
	})
	lastKBlockHeightGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "last_kblock_height",
		Help: "Height of last k-block",
	})
	blocksCommitedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "blocks_commited_total",
		Help: "Counter of commited blocks locally",
	})
)

type ConsensusConfig struct {
	ForceLastKFrame    bool
	SkipSignatureCheck bool
}

//-----------------------------------------------------------------------------
// ConsensusReactor defines a reactor for the consensus service.
type ConsensusReactor struct {
	chain        *chain.Chain
	stateCreator *state.Creator

	config   ConsensusConfig
	SyncDone bool

	// copy of master/node
	myPubKey      ecdsa.PublicKey  // this is my public identification !!
	myPrivKey     ecdsa.PrivateKey // copy of private key
	myBeneficiary meter.Address

	// still references above consensuStae, reactor if this node is
	// involved the consensus
	csMode             byte // delegates, committee, other
	configDelegateSize int  // configured delegate size.
	delegateSize       int  // global constant, current available delegate size. delegateSize <= configDelegateSize
	committeeSize      int
	myDelegatesIndex   int                 // this index will be changed by DelegateSet every time
	curDelegates       *types.DelegateSet  // current delegates list
	curCommittee       *types.ValidatorSet // This is top 400 of delegates by given nonce
	curActualCommittee []CommitteeMember   // Real committee, should be subset of curCommittee if someone is offline.
	curCommitteeIndex  int
	logger             log15.Logger

	csRoleInitialized uint
	csCommon          *ConsensusCommon //this must be allocated as validator
	csLeader          *ConsensusLeader
	//	csProposer        *ConsensusProposer
	csValidator *ConsensusValidator
	csPacemaker *Pacemaker

	// store key states here
	lastKBlockHeight uint32
	curNonce         uint64
	curEpoch         uint64
	curHeight        int64 // come from parentBlockID first 4 bytes uint32
	curRound         int
	mtx              sync.RWMutex

	// TODO: remove this, not used anymore
	kBlockData *block.KBlockData
	// consensus state for new consensus, similar to old conS

	// state changes may be triggered by: msgs from peers,
	// msgs from ourself, or by timeouts
	peerMsgQueue     chan consensusMsgInfo
	internalMsgQueue chan consensusMsgInfo
	schedulerQueue   chan func()

	// kBlock data
	KBlockDataQueue    chan block.KBlockData // from POW simulation
	RcvKBlockInfoQueue chan RecvKBlockInfo   // this channel for kblock notify from node module.

	newCommittee     *NewCommittee                     //New committee for myself
	rcvdNewCommittee map[NewCommitteeKey]*NewCommittee // store received new committee info

	dataDir string
	myAddr  types.NetAddress

	magic [4]byte
}

// Glob Instance
func GetConsensusGlobInst() *ConsensusReactor {
	return ConsensusGlobInst
}

func SetConsensusGlobInst(inst *ConsensusReactor) {
	ConsensusGlobInst = inst
}

// NewConsensusReactor returns a new ConsensusReactor with the given
// consensusState.
func NewConsensusReactor(ctx *cli.Context, chain *chain.Chain, state *state.Creator, privKey *ecdsa.PrivateKey, pubKey *ecdsa.PublicKey, magic [4]byte) *ConsensusReactor {
	conR := &ConsensusReactor{
		chain:        chain,
		stateCreator: state,
		logger:       log15.New("pkg", "consensus"),
		SyncDone:     false,
		magic:        magic,
	}

	if ctx != nil {
		conR.config = ConsensusConfig{
			ForceLastKFrame:    ctx.Bool("force-last-kframe"),
			SkipSignatureCheck: ctx.Bool("skip-signature-check"),
		}
	}

	//initialize message channel
	conR.peerMsgQueue = make(chan consensusMsgInfo, CHAN_DEFAULT_BUF_SIZE)
	conR.internalMsgQueue = make(chan consensusMsgInfo, CHAN_DEFAULT_BUF_SIZE)
	conR.schedulerQueue = make(chan func(), CHAN_DEFAULT_BUF_SIZE)
	conR.KBlockDataQueue = make(chan block.KBlockData, CHAN_DEFAULT_BUF_SIZE)

	// add the hardcoded genesis nonce in the case every node in block 0
	conR.RcvKBlockInfoQueue = make(chan RecvKBlockInfo, CHAN_DEFAULT_BUF_SIZE)

	//initialize height/round
	conR.lastKBlockHeight = chain.BestBlock().Header().LastKBlockHeight()
	conR.curHeight = int64(chain.BestBlock().Header().Number())
	conR.curRound = 0

	// initialize pacemaker
	conR.csPacemaker = NewPaceMaker(conR)

	// committee info is stored in the first of Mblock after Kblock
	if conR.curHeight != 0 {
		b, err := conR.chain.GetTrunkBlock(conR.lastKBlockHeight + 1)
		if err != nil {
			conR.logger.Error("get committee info block error")
			return nil
		}
		conR.logger.Info("get committeeinfo from block", "height", b.Header().Number())
		conR.curEpoch = b.GetCommitteeEpoch()
	} else {
		conR.curEpoch = 0
	}

	prometheus.MustRegister(curRoundGauge)

	prometheus.MustRegister(lastKBlockHeightGauge)
	prometheus.MustRegister(blocksCommitedCounter)
	prometheus.MustRegister(inCommitteeGauge)
	prometheus.MustRegister(pmRoleGauge)

	lastKBlockHeightGauge.Set(float64(conR.lastKBlockHeight))

	//initialize Delegates
	conR.committeeSize = ctx.Int("committee-size")     // 4 //COMMITTEE_SIZE
	conR.configDelegateSize = ctx.Int("delegate-size") // 10 //DELEGATES_SIZE
	dataDir := ctx.String("data-dir")
	conR.dataDir = dataDir
	conR.curDelegates = types.NewDelegateSet(GetConsensusDelegates(dataDir, conR.configDelegateSize, conR.committeeSize))
	conR.delegateSize = len(conR.curDelegates.Delegates)

	conR.rcvdNewCommittee = make(map[NewCommitteeKey]*NewCommittee, 10)

	conR.myPrivKey = *privKey
	conR.myPubKey = *pubKey

	SetConsensusGlobInst(conR)
	return conR
}

// OnStart implements BaseService by subscribing to events, which later will be
// broadcasted to other peers and starting state if we're not in fast sync.
func (conR *ConsensusReactor) OnStart() error {
	// Start new consensus
	conR.NewConsensusStart()

	// force to receive nonce
	//conR.ConsensusHandleReceivedNonce(0, 1001)

	conR.logger.Info("Consensus started ... ", "curHeight", conR.curHeight, "curRound", conR.curRound)
	return nil
}

// OnStop implements BaseService by unsubscribing from events and stopping
// state.
func (conR *ConsensusReactor) OnStop() {
	// New consensus
	conR.NewConsensusStop()
}

func (conR *ConsensusReactor) GetLastKBlockHeight() uint32 {
	return conR.lastKBlockHeight
}

// SwitchToConsensus switches from fast_sync mode to consensus mode.
// It resets the state, turns off fast_sync, and starts the consensus state-machine
func (conR *ConsensusReactor) SwitchToConsensus() {
	//conR.Logger.Info("SwitchToConsensus")
	conR.logger.Info("Synchnization is done. SwitchToConsensus ...")

	var replay bool
	var nonce uint64
	best := conR.chain.BestBlock()

	// special handle genesis.
	if best.Header().Number() == 0 {
		nonce = genesis.GenesisNonce
		replay = false
		conR.ConsensusHandleReceivedNonce(int64(best.Header().Number()), nonce, replay)
		return
	}

	// --force-last-kframe
	if !conR.config.ForceLastKFrame {
		return
	}

	// best is kblock, use this kblock
	if best.Header().BlockType() == block.BLOCK_TYPE_K_BLOCK {
		kBlockData, err := best.GetKBlockData()
		if err != nil {
			panic("can't get KBlockData")
		}
		nonce = kBlockData.Nonce
		replay = false
		conR.ConsensusHandleReceivedNonce(int64(best.Header().Number()), nonce, replay)
	} else {
		// mblock
		lastKBlockHeight := best.Header().LastKBlockHeight()
		lastKBlockHeightGauge.Set(float64(lastKBlockHeight))

		if lastKBlockHeight == 0 {
			nonce = genesis.GenesisNonce
		} else {
			kblock, err := conR.chain.GetTrunkBlock(lastKBlockHeight)
			if err != nil {
				panic(fmt.Sprintf("get last kblock %v failed", lastKBlockHeight))
			}

			kBlockData, err := kblock.GetKBlockData()
			if err != nil {
				panic("can't get KBlockData")
			}
			nonce = kBlockData.Nonce
		}

		//mark the flag of replay. should initialize by existed the BLS system
		replay = true
		conR.ConsensusHandleReceivedNonce(int64(lastKBlockHeight), nonce, replay)
	}

	return
}

// String returns a string representation of the ConsensusReactor.
// NOTE: For now, it is just a hard-coded string to avoid accessing unprotected shared variables.
// TODO: improve!
func (conR *ConsensusReactor) String() string {
	// better not to access shared variables
	return "ConsensusReactor" // conR.StringIndented("")
}

/**********************
// StringIndented returns an indented string representation of the ConsensusReactor
func (conR *ConsensusReactor) StringIndented(indent string) string {
	s := "ConsensusReactor{\n"
	s += indent + "  " + conR.conS.StringIndented(indent+"  ") + "\n"
	for _, peer := range conR.Switch.Peers().List() {
		ps := peer.Get(types.PeerStateKey).(*PeerState)
		s += indent + "  " + ps.StringIndented(indent+"  ") + "\n"
	}
	s += indent + "}"
	return s
}
************************/

//-----------------------------------------------------------------------------
//----new consensus-------------------------------------------------------------
//-----------------------------------------------------------------------------
//define the node mode
// 1. CONSENSUS_MODE_OTHER
// 2. CONSENSUS_MODE_OBSERVER
// 3. CONSENSUS_MODE_DELEGATE
// 4. CONSENSUS_MODE_COMMITTEE

const (
	CONSENSUS_MODE_OTHER     = byte(0x01)
	CONSENSUS_MODE_OBSERVER  = byte(0x02)
	CONSENSUS_MODE_DELEGATE  = byte(0x03)
	CONSENSUS_MODE_COMMITTEE = byte(0x04)

	// Flags of Roles
	CONSENSUS_COMMIT_ROLE_NONE      = uint(0x0)
	CONSENSUS_COMMIT_ROLE_LEADER    = uint(0x01)
	CONSENSUS_COMMIT_ROLE_PROPOSER  = uint(0x02)
	CONSENSUS_COMMIT_ROLE_VALIDATOR = uint(0x04)

	//Consensus Message Type
	CONSENSUS_MSG_ANNOUNCE_COMMITTEE          = byte(0x01)
	CONSENSUS_MSG_COMMIT_COMMITTEE            = byte(0x02)
	CONSENSUS_MSG_PROPOSAL_BLOCK              = byte(0x03)
	CONSENSUS_MSG_NOTARY_ANNOUNCE             = byte(0x04)
	CONSENSUS_MSG_NOTARY_BLOCK                = byte(0x05)
	CONSENSUS_MSG_VOTE_FOR_PROPOSAL           = byte(0x06)
	CONSENSUS_MSG_VOTE_FOR_NOTARY             = byte(0x07)
	CONSENSUS_MSG_MOVE_NEW_ROUND              = byte(0x08)
	CONSENSUS_MSG_PACEMAKER_PROPOSAL          = byte(0x09)
	CONSENSUS_MSG_PACEMAKER_VOTE_FOR_PROPOSAL = byte(0x10)
	CONSENSUS_MSG_PACEMAKER_NEW_VIEW          = byte(0x11)
	CONSENSUS_MSG_NEW_COMMITTEE               = byte(0x12)
	CONSENSUS_MSG_PACEMAKER_QUERY_PROPOSAL    = byte(0x13)
)

// CommitteeMember is validator structure + consensus fields
type CommitteeMember struct {
	Address     meter.Address
	PubKey      ecdsa.PublicKey
	VotingPower int64
	CommitKey   []byte
	NetAddr     types.NetAddress
	CSPubKey    bls.PublicKey
	CSIndex     int
}

// create new committee member
func NewCommitteeMember() *CommitteeMember {
	return &CommitteeMember{}
}

func (cm *CommitteeMember) ToString() string {
	return fmt.Sprintf("[CommitteeMember: Address:%s PubKey:%s VotingPower:%v CSPubKey:%s, CSIndex:%v]",
		cm.Address.String(), hex.EncodeToString(crypto.FromECDSAPub(&cm.PubKey)),
		cm.VotingPower, cm.CSPubKey.ToString(), cm.CSIndex)
}

type consensusMsgInfo struct {
	//Msg    ConsensusMessage
	Msg    []byte
	csPeer *ConsensusPeer
}

// set CS mode
func (conR *ConsensusReactor) setCSMode(nMode byte) bool {
	conR.csMode = nMode
	return true
}

func (conR *ConsensusReactor) getCSMode() byte {
	return conR.csMode
}

func (conR *ConsensusReactor) isCSCommittee() bool {
	return (conR.csMode == CONSENSUS_MODE_COMMITTEE)
}

func (conR *ConsensusReactor) isCSDelegates() bool {
	return (conR.csMode == CONSENSUS_MODE_DELEGATE)
}

func (conR *ConsensusReactor) UpdateHeight(height int64) bool {
	conR.logger.Info(fmt.Sprintf("Update conR.curHeight from %d to %d", conR.curHeight, height))
	conR.curHeight = height
	return true
}

func (conR *ConsensusReactor) UpdateRound(round int) bool {
	conR.logger.Info(fmt.Sprintf("Update conR.curRound from %d to %d", conR.curRound, round))
	conR.curRound = round

	return true
}

// update the Height
func (conR *ConsensusReactor) UpdateHeightRound(height int64, round int) bool {
	if height != 0 {
		conR.curHeight = height
	}

	conR.curRound = round
	return true
}

// update the LastKBlockHeight
func (conR *ConsensusReactor) UpdateLastKBlockHeight(height uint32) bool {
	conR.lastKBlockHeight = height
	lastKBlockHeightGauge.Set(float64(height))
	return true
}

// Refresh the current Height from the best block
// normally call this routine after block chain changed
func (conR *ConsensusReactor) RefreshCurHeight() error {
	prev := conR.curHeight

	bestHeader := conR.chain.BestBlock().Header()
	conR.curHeight = int64(bestHeader.Number())
	conR.lastKBlockHeight = bestHeader.LastKBlockHeight()

	lastKBlockHeightGauge.Set(float64(conR.lastKBlockHeight))

	conR.logger.Info("Refresh curHeight", "previous", prev, "now", conR.curHeight, "lastKBlockHeight", conR.lastKBlockHeight)
	return nil
}

// after announce/commit, Leader got the actual committee, which is the subset of curCommittee if some committee member offline.
// indexs and pubKeys are not sorted slice, AcutalCommittee must be sorted.
// Only Leader can call this method. indexes do not include the leader itself.
func (conR *ConsensusReactor) UpdateActualCommittee(indexes []int, pubKeys []bls.PublicKey, bitArray *cmn.BitArray) bool {

	if len(indexes) != len(pubKeys) ||
		len(indexes) > conR.committeeSize {
		conR.logger.Error("failed to update reactor actual committee ...")
		return false
	}

	// Add leader (myself) to the AcutalCommittee,
	// if there is time out, myself is not the first one in curCommittee
	l := conR.curCommittee.Validators[conR.curCommitteeIndex]
	cm := CommitteeMember{
		Address:     l.Address,
		PubKey:      l.PubKey,
		VotingPower: l.VotingPower,
		CommitKey:   l.CommitKey,
		NetAddr:     l.NetAddr,
		CSPubKey:    conR.csCommon.PubKey, //bls PublicKey
		CSIndex:     conR.curCommitteeIndex,
	}
	conR.curActualCommittee = append(conR.curActualCommittee, cm)

	for i, index := range indexes {
		//sanity check
		if (index == -1) || (index > conR.committeeSize) {
			// fmt.Println(i, "index", index)
			continue
		}

		//get validator info
		v := conR.curCommittee.Validators[index]

		cm := CommitteeMember{
			Address:     v.Address,
			PubKey:      v.PubKey,
			VotingPower: v.VotingPower,
			CommitKey:   v.CommitKey,
			NetAddr:     v.NetAddr,
			CSPubKey:    pubKeys[i], //bls PublicKey
			CSIndex:     index,
		}
		conR.curActualCommittee = append(conR.curActualCommittee, cm)
	}

	if len(conR.curActualCommittee) == 0 {
		return false
	}

	// Sort them.
	sort.SliceStable(conR.curActualCommittee, func(i, j int) bool {
		return (bytes.Compare(conR.curActualCommittee[i].CommitKey, conR.curActualCommittee[j].CommitKey) <= 0)
	})

	// I am Leader, first one should be myself.
	if bytes.Equal(crypto.FromECDSAPub(&conR.curActualCommittee[0].PubKey), crypto.FromECDSAPub(&conR.myPubKey)) == false {
		conR.logger.Error("I am leader and not in first place of curActualCommittee, must correct !!!")
		return false
	}

	return true
}

// get current round proposer
func (conR *ConsensusReactor) getCurrentProposer() CommitteeMember {
	size := len(conR.curActualCommittee)
	if size == 0 {
		return CommitteeMember{}
	}
	return conR.curActualCommittee[conR.curRound%len(conR.curActualCommittee)]
}

// get the specific round proposer
func (conR *ConsensusReactor) getRoundProposer(round int) CommitteeMember {
	size := len(conR.curActualCommittee)
	if size == 0 {
		return CommitteeMember{}
	}
	return conR.curActualCommittee[round%size]
}

func (conR *ConsensusReactor) amIRoundProproser(round uint64) bool {
	p := conR.getRoundProposer(int(round))
	return bytes.Equal(crypto.FromECDSAPub(&p.PubKey), crypto.FromECDSAPub(&conR.myPubKey))
}

//create validatorSet by a given nonce. return by my self role
func (conR *ConsensusReactor) NewValidatorSetByNonce(nonce uint64) (uint, bool) {
	//vals []*types.Validator

	committee, role, index, inCommittee := conR.CalcCommitteeByNonce(nonce)
	conR.curCommittee = committee
	if inCommittee == true {
		conR.csMode = CONSENSUS_MODE_COMMITTEE
		conR.curCommitteeIndex = index
		conR.myAddr = conR.curCommittee.Validators[index].NetAddr
		conR.logger.Info("New committee calculated", "index", index, "role", role)
		fmt.Println(committee)
	} else {
		conR.csMode = CONSENSUS_MODE_DELEGATE
		conR.curCommitteeIndex = 0
		// FIXME: find a better way
		conR.myAddr = types.NetAddress{IP: net.IP{}, Port: 8670}
		conR.logger.Info("New committee calculated")
		fmt.Println(committee)
	}

	return role, inCommittee
}

//This is similar routine of NewValidatorSetByNonce.
//it is used for temp calculate committee set by a given nonce in the fly.
// also return the committee
func (conR *ConsensusReactor) CalcCommitteeByNonce(nonce uint64) (*types.ValidatorSet, uint, int, bool) {
	buf := make([]byte, binary.MaxVarintLen64)
	binary.PutUvarint(buf, nonce)

	vals := make([]*types.Validator, 0)
	for _, d := range conR.curDelegates.Delegates {
		v := &types.Validator{
			Name:        string(d.Name),
			Address:     d.Address,
			PubKey:      d.PubKey,
			VotingPower: d.VotingPower,
			NetAddr:     d.NetAddr,
			CommitKey:   crypto.Keccak256(append(crypto.FromECDSAPub(&d.PubKey), buf...)),
		}
		vals = append(vals, v)
	}

	sort.SliceStable(vals, func(i, j int) bool {
		return (bytes.Compare(vals[i].CommitKey, vals[j].CommitKey) <= 0)
	})

	// the full list is stored in currCommittee, sorted.
	// To become a validator (real member in committee), must repond the leader's
	// announce. Validators are stored in conR.conS.Vlidators
	Committee := types.NewValidatorSet2(vals[:conR.committeeSize])
	if bytes.Equal(crypto.FromECDSAPub(&vals[0].PubKey), crypto.FromECDSAPub(&conR.myPubKey)) == true {
		return Committee, CONSENSUS_COMMIT_ROLE_LEADER, 0, true
	}

	for i, val := range vals {
		if bytes.Equal(crypto.FromECDSAPub(&val.PubKey), crypto.FromECDSAPub(&conR.myPubKey)) == true {
			return Committee, CONSENSUS_COMMIT_ROLE_VALIDATOR, i, true
		}
	}

	return Committee, CONSENSUS_COMMIT_ROLE_NONE, 0, false
}

func (conR *ConsensusReactor) GetCommitteeMemberIndex(pubKey ecdsa.PublicKey) int {
	for i, v := range conR.curCommittee.Validators {
		if bytes.Equal(crypto.FromECDSAPub(&v.PubKey), crypto.FromECDSAPub(&pubKey)) == true {
			return i
		}
	}
	conR.logger.Error("I'm not in committee, please check public key settings", "pubKey", pubKey)
	return -1
}

func (conR *ConsensusReactor) GetActualCommitteeMemberIndex(pubKey *ecdsa.PublicKey) int {
	for i, member := range conR.curActualCommittee {
		if bytes.Equal(crypto.FromECDSAPub(&member.PubKey), crypto.FromECDSAPub(pubKey)) == true {
			return i
		}
	}
	conR.logger.Error("public key not found in actual committee", "pubKey", pubKey)
	return -1
}

// input is serialized ecdsa.PublicKey
func (conR *ConsensusReactor) GetCommitteeMember(pubKey []byte) *CommitteeMember {
	for _, v := range conR.curActualCommittee {
		if bytes.Equal(crypto.FromECDSAPub(&v.PubKey), pubKey) == true {
			return &v
		}
	}
	conR.logger.Error("not found", "pubKey", pubKey)
	return nil
}

// Handle received Message
func (conR *ConsensusReactor) handleMsg(mi consensusMsgInfo) {
	conR.mtx.Lock()
	defer conR.mtx.Unlock()

	rawMsg, peer := mi.Msg, mi.csPeer

	msg, err := decodeMsg(rawMsg)
	if err != nil {
		conR.logger.Error("Error decoding message", "src", peer, "msg", msg, "err", err, "bytes", rawMsg)
		return
	}

	typeName := reflect.TypeOf(msg).String()
	if strings.Contains(typeName, ".") {
		typeName = strings.Split(typeName, ".")[1]
	}
	conR.logger.Info(fmt.Sprintf("Received %v from peer", typeName),
		"length", len(rawMsg),
		"ip", peer.netAddr.IP.String())

	switch msg := msg.(type) {

	// New consensus Messages
	case *AnnounceCommitteeMessage:
		if (conR.csRoleInitialized&CONSENSUS_COMMIT_ROLE_VALIDATOR) == 0 ||
			(conR.csValidator == nil) {
			conR.logger.Warn("not in validator role, enter validator first ...")
			// if find out we are not in committee, then exit validator
			conR.enterConsensusValidator()
		}

		success := conR.csValidator.ProcessAnnounceCommittee(msg, peer)
		// For ProcessAnnounceCommittee, it is not validator if return is false
		if success == false {
			conR.logger.Error("process announce failed")
			conR.exitConsensusValidator()
		}

	case *CommitCommitteeMessage:
		if (conR.csRoleInitialized&CONSENSUS_COMMIT_ROLE_LEADER) == 0 ||
			(conR.csLeader == nil) {
			conR.logger.Warn("not in leader role, ignore CommitCommitteeMessage")
			break
		}

		success := conR.csLeader.ProcessCommitMsg(msg, peer)
		if success == false {
			conR.logger.Error("process CommitCommitteeMessage failed")
		}

	case *NotaryAnnounceMessage:
		if (conR.csRoleInitialized&CONSENSUS_COMMIT_ROLE_VALIDATOR) == 0 ||
			(conR.csValidator == nil) {
			conR.logger.Warn("not in validator role, ignore NotaryAnnounceMessage")
			break
		}

		success := conR.csValidator.ProcessNotaryAnnounceMessage(msg, peer)
		if success == false {
			conR.logger.Error("process NotaryAnnounceMessage failed")
		}

	case *VoteForNotaryMessage:
		ch := msg.CSMsgCommonHeader

		if ch.MsgSubType == VOTE_FOR_NOTARY_ANNOUNCE {
			// vote for notary announce
			if (conR.csRoleInitialized&CONSENSUS_COMMIT_ROLE_LEADER) == 0 ||
				(conR.csLeader == nil) {
				conR.logger.Warn("not in leader role, ignore VoteForNotaryMessage")
				break
			}

			success := conR.csLeader.ProcessVoteNotaryAnnounce(msg, peer)
			if success == false {
				conR.logger.Error("process VoteForNotary(Announce) failed")
			}

		} else {
			conR.logger.Error("Unknown MsgSubType", "value", ch.MsgSubType)
		}

	case *NewCommitteeMessage:
		success := conR.ProcessNewCommitteeMessage(msg, peer)
		if success == false {
			conR.logger.Error("process NewcommitteeMessage failed")
		}
	default:
		conR.logger.Error("Unknown msg type", "value", reflect.TypeOf(msg))
	}
}

// receiveRoutine handles messages which may cause state transitions.
func (conR *ConsensusReactor) receiveRoutine() {
	/******
	onExit := func(conR *ConsensusReactor) {
		// NOTE: the internalMsgQueue may have signed messages from our
		// fmt.Println("Exiting receiveRoutine ... ", "height ", conR.curHeight, "round ", conR.curRound)
		return
	}
	*******/
	//wait for synchronization is done
	communicator := comm.GetGlobCommInst()
	if communicator == nil {
		conR.logger.Error("get communicator instance failed ...")
		return
	}
	select {
	case <-communicator.Synced():
		conR.SwitchToConsensus()
	}
	conR.logger.Info("Sync is done, start to accept consensus message")
	conR.SyncDone = true

	for {
		var mi consensusMsgInfo
		select {
		case mi = <-conR.peerMsgQueue:
			// handles proposals, block parts, votes
			// may generate internal events (votes, complete proposals, 2/3 majorities)
			// conR.logger.Debug("Received message from peerMsgQueue")
			conR.handleMsg(mi)
		case mi = <-conR.internalMsgQueue:
			// handles proposals, block parts, votes
			conR.logger.Debug("Received message from InternalMsgQueue")
			conR.handleMsg(mi)
		case ti := <-conR.schedulerQueue:
			conR.HandleSchedule(ti)

		case ki := <-conR.RcvKBlockInfoQueue:
			conR.HandleRecvKBlockInfo(ki)

		case kd := <-conR.KBlockDataQueue:
			conR.HandleKBlockData(kd)

			/*******
			case pi := <-conR.packerInfoQueue:
				conR.HandlePackerInfo(pi)
			case <-conR.Quit():
				onExit(conR)
			************/
		}
	}
}

func (conR *ConsensusReactor) receivePeerMsg(w http.ResponseWriter, r *http.Request) {
	var base = 10
	var size = 16
	defer r.Body.Close()
	var params map[string]string
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		fmt.Errorf("%v\n", err)
		respondWithJson(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if _, ok := params["magic"]; !ok {
		conR.logger.Debug("ignored message due to missing magic", "expect", hex.EncodeToString(conR.magic[:]))
		return
	}
	if strings.Compare(params["magic"], hex.EncodeToString(conR.magic[:])) != 0 {
		conR.logger.Debug("ignored message due to magic mismatch", "expect", hex.EncodeToString(conR.magic[:]), "actual", params["magic"])
		return
	}
	peerIP := net.ParseIP(params["peer_ip"])
	peerPort, convErr := strconv.ParseUint(params["port"], base, size)
	if convErr != nil {
		fmt.Errorf("Failed to convert to uint.")
	}
	peerPortUint16 := uint16(peerPort)
	p := newConsensusPeer(peerIP, peerPortUint16, conR.magic)
	// p := ConsensusPeer{netAddr: peerAddr}
	msgByteSlice, _ := hex.DecodeString(params["message"])
	mi := consensusMsgInfo{
		Msg:    msgByteSlice,
		csPeer: p,
	}
	conR.peerMsgQueue <- mi
	respondWithJson(w, http.StatusOK, map[string]string{"result": "success"})
}

func respondWithJson(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func (conR *ConsensusReactor) receiveConsensusMsgRoutine() {

	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},    // All origins
		AllowedMethods: []string{"POST"}, // Only allows POST requests
	})
	r := mux.NewRouter()
	r.HandleFunc("/consensus", conR.receivePeerMsg).Methods("POST")
	r.HandleFunc("/pacemaker", conR.csPacemaker.receivePacemakerMsg).Methods("POST")
	if err := http.ListenAndServe(":8670", c.Handler(r)); err != nil {
		fmt.Errorf("HTTP receiver error!")
	}
}

//Entry point of new consensus
func (conR *ConsensusReactor) NewConsensusStart() int {
	conR.logger.Debug("Starting New Consensus ...")

	/***** XXX: Common init is based on roles
	 ***** Leader generate bls type/params/system and send out those params
	 ***** by announce message. Validators receives announce and do common init

	// initialize consensus common, Common is calling the C libary,
	// need to deinit to avoid the memory leak
	conR.csCommon = NewConsensusCommon(conR)
	******/

	// Uncomment following to enable peer messages between nodes
	go conR.receiveConsensusMsgRoutine()

	// pacemaker
	// go conR.receivePacemakerMsgRoutine()

	// Start receive routine
	go conR.receiveRoutine() //only handles from channel
	return 0
}

// called by reactor stop
func (conR *ConsensusReactor) NewConsensusStop() int {
	conR.logger.Warn("Stop New Consensus ...")

	// Deinitialize consensus common
	conR.csCommon.ConsensusCommonDeinit()
	return 0
}

// -------
// Enter validator
func (conR *ConsensusReactor) enterConsensusValidator() int {
	conR.logger.Debug("Enter consensus validator")

	conR.csValidator = NewConsensusValidator(conR)
	conR.csRoleInitialized |= CONSENSUS_COMMIT_ROLE_VALIDATOR
	conR.csValidator.replay = conR.newCommittee.Replay //replay flag carried by newcommittee

	return 0
}

func (conR *ConsensusReactor) exitConsensusValidator() int {

	conR.csValidator = nil
	conR.csRoleInitialized &= ^CONSENSUS_COMMIT_ROLE_VALIDATOR
	conR.logger.Debug("Exit consensus validator")
	return 0
}

// Enter leader
func (conR *ConsensusReactor) enterConsensusLeader(epochID uint64) {
	conR.logger.Debug("Enter consensus leader", "epochID", epochID)
	if epochID <= conR.curEpoch {
		conR.logger.Warn("enterConsensusLeader: epochID less than current Epoch, do not update leader", "curEpochID", conR.curEpoch, "epochID", epochID)
		return
	}

	// init consensus common as leader
	// need to deinit to avoid the memory leak
	if conR.csCommon != nil {
		conR.csCommon.ConsensusCommonDeinit()
	}

	conR.csCommon = NewConsensusCommon(conR)

	conR.csLeader = NewCommitteeLeader(conR)
	conR.csRoleInitialized |= CONSENSUS_COMMIT_ROLE_LEADER

	conR.curEpoch = epochID
	conR.csLeader.EpochID = epochID
	return
}

func (conR *ConsensusReactor) exitConsensusLeader(epochID uint64) {
	conR.logger.Warn("Exit consensus leader", "epochID", epochID)
	if conR.curEpoch != epochID {
		conR.logger.Warn("exitConsensusLeader: epochID mismatch, do not update leader", "curEpochID", conR.curEpoch, "epochID", epochID)
		return
	}
	conR.csLeader = nil
	conR.csRoleInitialized &= ^CONSENSUS_COMMIT_ROLE_LEADER

	return
}

// Cleanup all roles before the comittee relay
func (conR *ConsensusReactor) exitCurCommittee() error {

	conR.exitConsensusLeader(conR.curEpoch)
	conR.exitConsensusValidator()
	// Only node in committee did initilize common
	if conR.csCommon != nil {
		conR.csCommon.ConsensusCommonDeinit()
		conR.csCommon = nil
	}

	// clean up current parameters
	if conR.curCommittee != nil {
		conR.curCommittee.Validators = make([]*types.Validator, 0)
	}
	conR.curActualCommittee = make([]CommitteeMember, 0)
	conR.curCommitteeIndex = 0
	conR.kBlockData = nil

	conR.curNonce = 0
	conR.curRound = 0

	return nil
}

func getConcreteName(msg ConsensusMessage) string {
	switch msg.(type) {
	case *AnnounceCommitteeMessage:
		return "AnnounceCommitteeMessage"
	case *CommitCommitteeMessage:
		return "CommitCommitteeMessage"
	case *NotaryAnnounceMessage:
		return "NotaryAnnounceMessage"
	case *VoteForNotaryMessage:
		return "VoteForNotaryMessage"

	case *PMProposalMessage:
		return "PMProposalMessage"
	case *PMVoteForProposalMessage:
		return "PMVoteForProposalMessage"
	case *PMNewViewMessage:
		return "PMNewViewMessage"
	case *NewCommitteeMessage:
		return "NewCommitteeMessage"
	}
	return ""
}

func (conR *ConsensusReactor) SendMsgToPeers(csPeers []*ConsensusPeer, msg *ConsensusMessage) bool {
	//var wg sync.WaitGroup
	for _, p := range csPeers {
		//wg.Add(1)
		go func(msg *ConsensusMessage, p *ConsensusPeer) {
			//defer wg.Done()
			conR.sendConsensusMsg(msg, p)
		}(msg, p)
	}

	//wg.Wait()
	return true
}

func (conR *ConsensusReactor) GetMyNetAddr() types.NetAddress {
	return conR.myAddr
	// return conR.curCommittee.Validators[conR.curCommitteeIndex].NetAddr
}

func (conR *ConsensusReactor) GetMyPeers() ([]*ConsensusPeer, error) {
	peers := make([]*ConsensusPeer, 0)
	myNetAddr := conR.GetMyNetAddr()
	for _, member := range conR.curActualCommittee {
		if member.NetAddr.IP.String() != myNetAddr.IP.String() {
			peers = append(peers, newConsensusPeer(member.NetAddr.IP, member.NetAddr.Port, conR.magic))
		}
	}
	return peers, nil
}

func (conR *ConsensusReactor) GetMyActualCommitteeIndex() int {
	myNetAddr := conR.GetMyNetAddr()
	for index, member := range conR.curActualCommittee {
		if member.NetAddr.IP.String() == myNetAddr.IP.String() {
			return index
		}
	}
	return -1
}

type ApiCommitteeMember struct {
	Name        string
	Address     meter.Address
	PubKey      string
	VotingPower int64
	NetAddr     string
	CsPubKey    string
	CsIndex     int
}

func (conR *ConsensusReactor) GetLatestCommitteeList() ([]*ApiCommitteeMember, error) {
	var committeeMembers []*ApiCommitteeMember
	for _, cm := range conR.curActualCommittee {
		delegate := conR.curCommittee.Validators[cm.CSIndex]
		apiCm := &ApiCommitteeMember{
			Name:        delegate.Name,
			Address:     cm.Address,
			PubKey:      b64.StdEncoding.EncodeToString(crypto.FromECDSAPub(&cm.PubKey)),
			VotingPower: cm.VotingPower,
			NetAddr:     cm.NetAddr.String(),
			CsPubKey:    hex.EncodeToString(conR.csCommon.system.PubKeyToBytes(cm.CSPubKey)),
			CsIndex:     cm.CSIndex,
		}
		committeeMembers = append(committeeMembers, apiCm)
	}
	return committeeMembers, nil
}

// XXX. For test only
func (conR *ConsensusReactor) sendConsensusMsg(msg *ConsensusMessage, csPeer *ConsensusPeer) bool {
	typeName := getConcreteName(*msg)

	rawMsg := cdc.MustMarshalBinaryBare(msg)
	if len(rawMsg) > maxMsgSize {
		fmt.Errorf("Msg exceeds max size (%d > %d)", len(rawMsg), maxMsgSize)
		conR.logger.Error("Msg exceeds max size", "rawMsg=", len(rawMsg), "maxMsgSize=", maxMsgSize)
		return false
	}

	conR.logger.Debug(fmt.Sprintf("Try send %v out", typeName), "size", len(rawMsg))
	// fmt.Println(hex.Dump(rawMsg))

	if csPeer == nil {
		conR.internalMsgQueue <- consensusMsgInfo{rawMsg, nil}
	} else {
		//conR.peerMsgQueue <- consensusMsgInfo{rawMsg, csPeer}
		/*************
		payload := map[string]interface{}{
			"message":   hex.EncodeToString(rawMsg),
			"peer_ip":   csPeer.netAddr.IP.String(),
			"peer_id":   string(csPeer.netAddr.ID),
			"peer_port": string(csPeer.netAddr.Port),
		}
		**************/
		// myNetAddr := conR.curCommittee.Validators[conR.curCommitteeIndex].NetAddr
		magicHex := hex.EncodeToString(conR.magic[:])
		payload := map[string]interface{}{
			"message": hex.EncodeToString(rawMsg),
			"peer_ip": conR.myAddr.IP.String(),
			//"peer_id":   string(myNetAddr.ID),
			"peer_port": string(conR.myAddr.Port),
			"magic":     magicHex,
		}

		jsonStr, err := json.Marshal(payload)
		if err != nil {
			fmt.Errorf("Failed to marshal message dict to json string")
			return false
		}

		var netClient = &http.Client{
			Timeout: time.Second * 4,
		}
		resp, err := netClient.Post("http://"+csPeer.netAddr.IP.String()+":8670/consensus", "application/json", bytes.NewBuffer(jsonStr))
		if err != nil {
			conR.logger.Error("Failed to send message to peer", "peer", csPeer.String(), "err", err)
			return false
		}
		conR.logger.Info(fmt.Sprintf("Sent %v to peer", typeName), "peer", csPeer.String(), "size", len(rawMsg))
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
	}
	return true
}

//============================================
// Signer extract signer of the block from signature.
func (conR *ConsensusReactor) ConsensusMsgSigner(msgHash, sig []byte) (ecdsa.PublicKey, error) {
	pub, err := crypto.SigToPub(msgHash, sig)
	if err != nil {
		return ecdsa.PublicKey{}, err
	}

	//signer = meter.Address(crypto.PubkeyToAddress(*pub))
	return *pub, nil
}

func (conR *ConsensusReactor) SignConsensusMsg(msgHash []byte) (sig []byte, err error) {
	sig, err = crypto.Sign(msgHash, &conR.myPrivKey)
	if err != nil {
		return []byte{}, err
	}

	return sig, nil
}

func (conR *ConsensusReactor) ValidateCMheaderSig(cmh *ConsensusMsgCommonHeader, msgHash []byte) bool {
	sender := cmh.Sender // send is byte slice format
	signer, err := conR.ConsensusMsgSigner(msgHash, cmh.Signature)
	if err != nil {
		conR.logger.Error("signature validate failed!", err)
	}

	if bytes.Equal(crypto.FromECDSAPub(&signer), sender) == false {
		conR.logger.Error("signature validate mismacth!")
		return false
	}
	return true
}

//----------------------------------------------------------------------------
// each node create signing message based on current information and sign part
// of them.

const (
	MSG_SIGN_OFFSET_DEFAULT = uint(0)
	MSG_SIGN_LENGTH_DEFAULT = uint(110)
)

// Sign Announce Committee
// "Announce Committee Message: Leader <pubkey 64(hexdump 32x2) bytes> EpochID <16 (8x2)bytes> Height <16 (8x2) bytes> Round <8(4x2)bytes>
func (conR *ConsensusReactor) BuildAnnounceSignMsg(pubKey ecdsa.PublicKey, epochID uint64, height uint64, round uint32) string {
	c := make([]byte, binary.MaxVarintLen64)
	binary.BigEndian.PutUint64(c, epochID)

	h := make([]byte, binary.MaxVarintLen64)
	binary.BigEndian.PutUint64(h, height)

	r := make([]byte, binary.MaxVarintLen32)
	binary.BigEndian.PutUint32(r, round)
	return fmt.Sprintf("%s %s %s %s %s %s %s %s", "Announce Committee Message: Leader", hex.EncodeToString(crypto.FromECDSAPub(&pubKey)),
		"EpochID", hex.EncodeToString(c), "Height", hex.EncodeToString(h),
		"Round", hex.EncodeToString(r))
}

// Sign Propopal Message
// "Proposal Block Message: BlockType <8 bytes> Height <16 (8x2) bytes> Round <8 (4x2) bytes>
func (conR *ConsensusReactor) BuildProposalBlockSignMsg(blockType uint32, height uint64, txsRoot, stateRoot *meter.Bytes32) string {
	c := make([]byte, binary.MaxVarintLen32)
	binary.BigEndian.PutUint32(c, blockType)

	h := make([]byte, binary.MaxVarintLen64)
	binary.BigEndian.PutUint64(h, height)

	return fmt.Sprintf("%s %s %s %s %s %s %s %s ",
		"BlockType", hex.EncodeToString(c),
		"Height", hex.EncodeToString(h),
		"TxRoot", txsRoot.String(),
		"StateRoot", stateRoot.String())
}

// Sign Notary Announce Message
// "Announce Notarization Message: Leader <pubkey 64(32x2)> EpochID <16 (8x2)bytes> Height <16 (8x2) bytes> Round <8 (4x2) bytes>
func (conR *ConsensusReactor) BuildNotaryAnnounceSignMsg(pubKey ecdsa.PublicKey, epochID uint64, height uint64, round uint32) string {
	c := make([]byte, binary.MaxVarintLen64)
	binary.BigEndian.PutUint64(c, epochID)

	h := make([]byte, binary.MaxVarintLen64)
	binary.BigEndian.PutUint64(h, height)

	r := make([]byte, binary.MaxVarintLen32)
	binary.BigEndian.PutUint32(r, round)
	return fmt.Sprintf("%s %s %s %s %s %s %s %s", "Announce Notarization Message: Leader", hex.EncodeToString(crypto.FromECDSAPub(&pubKey)),
		"EpochID", hex.EncodeToString(c), "Height", hex.EncodeToString(h),
		"Round", hex.EncodeToString(r))
}

// Sign Notary Block Message
// "Block Notarization Message: Proposer <pubkey 64(32x2)> BlockType <8 bytes> Height <16 (8x2) bytes> Round <8 (4x2) bytes>
func (conR *ConsensusReactor) BuildNotaryBlockSignMsg(pubKey ecdsa.PublicKey, blockType uint32, height uint64, round uint32) string {
	c := make([]byte, binary.MaxVarintLen32)
	binary.BigEndian.PutUint32(c, blockType)

	h := make([]byte, binary.MaxVarintLen64)
	binary.BigEndian.PutUint64(h, height)

	r := make([]byte, binary.MaxVarintLen32)
	binary.BigEndian.PutUint32(r, round)
	return fmt.Sprintf("%s %s %s %s %s %s %s %s", "Proposal Block Message: Proposer", hex.EncodeToString(crypto.FromECDSAPub(&pubKey)),
		"BlockType", hex.EncodeToString(c), "Height", hex.EncodeToString(h),
		"Round", hex.EncodeToString(r))
}

// Sign newRound
// "NewRound Message: Validator <pubkey 64(32x2)> EpochID <16(8x2) bytes> Height <16 (8x2) bytes> Counter <8 (4x2) bytes>
func (conR *ConsensusReactor) BuildNewRoundSignMsg(pubKey ecdsa.PublicKey, epochID uint64, height uint64, counter uint32) string {
	e := make([]byte, binary.MaxVarintLen64)
	binary.BigEndian.PutUint64(e, epochID)

	h := make([]byte, binary.MaxVarintLen64)
	binary.BigEndian.PutUint64(h, height)

	c := make([]byte, binary.MaxVarintLen32)
	binary.BigEndian.PutUint32(c, counter)

	return fmt.Sprintf("%s %s %s %s %s %s %s %s", "NewRound Message: Validator", hex.EncodeToString(crypto.FromECDSAPub(&pubKey)),
		"EpochID", hex.EncodeToString(e), "Height", hex.EncodeToString(h),
		"Counter", hex.EncodeToString(c))
}

// Sign Timeout
// "Timeout Message: Proposer <pubkey 64(32x2)> TimeoutRound <16(8x2) bytes> TimeoutHeight <16 (8x2) bytes> TimeoutCounter <8 (4x2) bytes>
func (conR *ConsensusReactor) BuildTimeoutSignMsg(pubKey ecdsa.PublicKey, round uint64, height uint64, counter uint32) string {
	r := make([]byte, binary.MaxVarintLen64)
	binary.BigEndian.PutUint64(r, round)

	h := make([]byte, binary.MaxVarintLen64)
	binary.BigEndian.PutUint64(h, height)

	c := make([]byte, binary.MaxVarintLen32)
	binary.BigEndian.PutUint32(c, counter)

	return fmt.Sprintf("%s %s %s %s %s %s %s %s", "Timeout Message: Proposer", hex.EncodeToString(crypto.FromECDSAPub(&pubKey)),
		"TimeoutRound", hex.EncodeToString(r), "Height", hex.EncodeToString(h),
		"TimeoutCounter", hex.EncodeToString(c))
}

//======end of New consensus =========================================
//-----------------------------------------------------------------------------
// New consensus timed schedule util
//type Scheduler func(conR *ConsensusReactor) bool
func (conR *ConsensusReactor) ScheduleLeader(epochID uint64, height uint64, d time.Duration) bool {
	time.AfterFunc(d, func() {
		conR.schedulerQueue <- func() { HandleScheduleLeader(conR, epochID, height) }
	})
	return true
}

func (conR *ConsensusReactor) ScheduleReplayLeader(epochID uint64, d time.Duration) bool {
	time.AfterFunc(d, func() {
		conR.schedulerQueue <- func() { HandleScheduleReplayLeader(conR, epochID) }
	})
	return true
}

func (conR *ConsensusReactor) ScheduleValidator(d time.Duration) bool {
	time.AfterFunc(d, func() {
		conR.schedulerQueue <- func() { HandleScheduleValidator(conR) }
	})
	return true
}

func (conR *ConsensusReactor) ScheduleReplayValidator(d time.Duration) bool {
	time.AfterFunc(d, func() {
		conR.schedulerQueue <- func() { HandleScheduleReplayValidator(conR) }
	})
	return true
}

// -------------------------------
func HandleScheduleReplayLeader(conR *ConsensusReactor, epochID uint64) bool {
	conR.exitConsensusLeader(conR.curEpoch)

	conR.logger.Debug("Enter consensus replay leader", "curEpochID", conR.curEpoch, "epochID", epochID)

	// init consensus common as leader
	// need to deinit to avoid the memory leak
	best := conR.chain.BestBlock()
	lastKBlockHeight := best.Header().LastKBlockHeight()
	lastKBlockHeightGauge.Set(float64(lastKBlockHeight))

	b, err := conR.chain.GetTrunkBlock(lastKBlockHeight + 1)
	if err != nil {
		conR.logger.Error("get committee info block error")
		return false
	}

	// committee members
	cis, err := b.GetCommitteeInfo()
	if err != nil {
		conR.logger.Error("decode committee info block error")
		return false
	}
	fmt.Println("cis", cis)

	systemBytes, _ := b.GetSystemBytes()
	paramsBytes, _ := b.GetParamsBytes()

	// to avoid memory leak
	if conR.csCommon != nil {
		conR.csCommon.ConsensusCommonDeinit()
		conR.csCommon = nil
	}
	conR.csCommon = NewReplayLeaderConsensusCommon(conR, paramsBytes, systemBytes)

	conR.csLeader = NewCommitteeLeader(conR)
	conR.csRoleInitialized |= CONSENSUS_COMMIT_ROLE_LEADER
	conR.csLeader.replay = true
	conR.curEpoch = epochID
	conR.csLeader.EpochID = epochID

	conR.csLeader.GenerateAnnounceMsg()
	return true
}

func HandleScheduleReplayValidator(conR *ConsensusReactor) bool {
	conR.exitConsensusValidator()

	conR.logger.Debug("Enter consensus replay validator")

	conR.csValidator = NewConsensusValidator(conR)
	conR.csRoleInitialized |= CONSENSUS_COMMIT_ROLE_VALIDATOR
	conR.csValidator.replay = true

	// Validator only responses the incoming message
	return true
}

func HandleScheduleLeader(conR *ConsensusReactor, epochID, height uint64) bool {
	curHeight := uint64(conR.chain.BestBlock().Header().Number())
	if curHeight != height {
		conR.logger.Error("ScheduleLeader: best height is different with kblock height", "curHeight", curHeight, "kblock height", height)
		if curHeight > height {
			// mine is ahead of kblock, stop
			return false
		}
		com := comm.GetGlobCommInst()
		if com == nil {
			conR.logger.Error("get global comm inst failed")
			return false
		}
		com.TriggerSync()
		conR.logger.Warn("Peer sync triggered")

		conR.ScheduleLeader(epochID, height, 1*time.Second)
		return false
	}

	conR.enterConsensusLeader(epochID)
	conR.csLeader.GenerateAnnounceMsg()
	return true
}

func HandleScheduleValidator(conR *ConsensusReactor) bool {
	conR.exitConsensusValidator()
	conR.enterConsensusValidator()

	// Validator only responses the incoming message
	return true
}

// Handle Schedules from conR.scheduleQueue
func (conR *ConsensusReactor) HandleSchedule(fn func()) bool {
	/***
	if ti.arg != conR {
		conR.logger.Debug("ConsensusReactor changed ...")
		return false
	}
	***/
	conR.logger.Debug("Handle schedule", "scheduling", fn)
	fn()
	return true
}

//////////////////////////////////////////////////////
// update current delegates with new delegates from staking
// keep this standalone method intentionly
func (conR *ConsensusReactor) ConsensusUpdateCurDelegates() {
	conR.curDelegates = types.NewDelegateSet(GetConsensusDelegates(conR.dataDir, conR.configDelegateSize, conR.committeeSize))
	conR.delegateSize = len(conR.curDelegates.Delegates)
}

// Consensus module handle received nonce from kblock
func (conR *ConsensusReactor) ConsensusHandleReceivedNonce(kBlockHeight int64, nonce uint64, replay bool) {
	conR.logger.Info("Received a nonce ...", "nonce", nonce, "kBlockHeight", kBlockHeight, "replay", replay)

	//conR.lastKBlockHeight = kBlockHeight
	conR.curNonce = nonce

	role, inCommittee := conR.NewValidatorSetByNonce(nonce)

	if inCommittee {
		conR.logger.Info("I am in committee!!!")

		info := &powpool.PowBlockInfo{}
		if kBlockHeight == 0 {
			info = powpool.GetPowGenesisBlockInfo()
		} else {
			kblock, _ := conR.chain.GetTrunkBlock(uint32(kBlockHeight))
			info = powpool.NewPowBlockInfoFromPosKBlock(kblock)
		}
		pool := powpool.GetGlobPowPoolInst()
		pool.Wash()
		pool.InitialAddKframe(info)
		conR.logger.Info("PowPool initial added kblock", "kblock height", kBlockHeight, "powHeight", info.PowHeight)

		if replay == true {
			//kblock is already added to pool, should start with next one
			startHeight := info.PowHeight + 1
			conR.logger.Info("Replay", "replay from powHeight", startHeight)
			pool.ReplayFrom(int32(startHeight))
		}
		inCommitteeGauge.Set(1)
	} else {
		inCommitteeGauge.Set(0)
		conR.logger.Info("I am NOT in committee!!!", "nonce", nonce)
	}

	// hotstuff: check the mechnism:
	// 1) send moveNewRound (with signature) to new leader. if new leader receives majority signature, then send out announce.
	if role == CONSENSUS_COMMIT_ROLE_LEADER {
		conR.logger.Info("I am committee leader for nonce!", "nonce", nonce)
		epochID := conR.curEpoch
		conR.exitConsensusLeader(epochID)
		conR.enterConsensusLeader(epochID + 1)

		// no replay case, the last block must be kblock!
		if replay == false && conR.curHeight != kBlockHeight {
			conR.logger.Error("the best block is not kblock", "curHeight", conR.curHeight, "kblock Height", kBlockHeight)
			return
		}

		conR.NewCommitteeInit(uint64(kBlockHeight), nonce, replay)

		//TBD:
		// wait 30 seconds for synchronization
		// time.Sleep(5 * WHOLE_NETWORK_BLOCK_SYNC_TIME)
		/***********
		time.Sleep(1 * WHOLE_NETWORK_BLOCK_SYNC_TIME)
		if replay {
			conR.ScheduleReplayLeader(0)
		} else {
			conR.ScheduleLeader(0)
		}
		***********/
		//wait for majority
	} else if role == CONSENSUS_COMMIT_ROLE_VALIDATOR {
		conR.logger.Info("I am committee validator for nonce!", "nonce", nonce)
		/*****
		if replay {
			conR.ScheduleReplayValidator(0)
		} else {
			conR.ScheduleValidator(0)
		}
		*****/
		// no replay case, the last block must be kblock!
		if replay == false && conR.curHeight != kBlockHeight {
			conR.logger.Error("the best block is not kblock", "curHeight", conR.curHeight, "kblock Height", kBlockHeight)
			return
		}

		conR.NewCommitteeInit(uint64(kBlockHeight), nonce, replay)
		newCommittee := conR.newCommittee
		nl := newCommittee.Committee.Validators[newCommittee.Round]
		leader := newConsensusPeer(nl.NetAddr.IP, nl.NetAddr.Port, conR.magic)
		leaderPubKey := nl.PubKey
		conR.sendNewCommitteeMessage(leader, leaderPubKey, newCommittee.KblockHeight,
			newCommittee.Nonce, newCommittee.Round)
		conR.NewCommitteeTimerStart()
	} else if role == CONSENSUS_COMMIT_ROLE_NONE {
		// even though it is not committee, still initialize NewCommittee for next
		conR.NewCommitteeInit(uint64(kBlockHeight), nonce, replay)
	}
	return
}

// newCommittee: start with new committee or not
// true --- with new committee, round = 0, best block is kblock.
// false ---replay mode, round continues with BestQC.QCRound. best block is mblock
// XXX: we assume the peers have the bestQC, if they don't ...
func (conR *ConsensusReactor) startPacemaker(newCommittee bool) error {
	// 1. bestQC height == best block height
	// 2. newCommittee is true, best block is kblock
	for i := 0; i < 3; i++ {
		conR.chain.UpdateBestQC()
		bestQC := conR.chain.BestQC()
		bestBlock := conR.chain.BestBlock()
		conR.logger.Info("Checking the QCHeight and Block height...", "QCHeight", bestQC.QCHeight, "BlockHeight", bestBlock.Header().Number())
		if bestQC.QCHeight != uint64(bestBlock.Header().Number()) {
			com := comm.GetGlobCommInst()
			if com == nil {
				conR.logger.Error("get global comm inst failed")
				return errors.New("pacemaker does not started")
			}
			com.TriggerSync()
			conR.logger.Warn("bestQC and bestBlock Height are not match ...")
			<-time.NewTimer(1 * time.Second).C
		} else {
			break
		}
	}
	conR.chain.UpdateBestQC()
	bestQC := conR.chain.BestQC()
	bestBlock := conR.chain.BestBlock()
	if bestQC.QCHeight != uint64(bestBlock.Header().Number()) {
		conR.logger.Error("bestQC and bestBlock still not match, Action (start pacemaker) cancelled ...")
		return nil
	}

	conR.logger.Info("startConsensusPacemaker", "QCHeight", bestQC.QCHeight, "BlockHeight", bestBlock.Header().Number())
	conR.csPacemaker.Start(newCommittee)
	return nil
}

// since votes of pacemaker include propser, but committee votes
// do not have leader itself, we seperate the majority func
// Easier adjust the logic of major 2/3, for pacemaker
func MajorityTwoThird(voterNum, committeeSize int) bool {
	if (voterNum < 0) || (committeeSize < 1) {
		fmt.Println("MajorityTwoThird, inputs out of range")
		return false
	}
	// Examples
	// committeeSize= 1 twoThirds= 1
	// committeeSize= 2 twoThirds= 2
	// committeeSize= 3 twoThirds= 2
	// committeeSize= 4 twoThirds= 3
	// committeeSize= 5 twoThirds= 4
	// committeeSize= 6 twoThirds= 4
	twoThirds := math.Ceil(float64(committeeSize) * 2 / 3)
	if float64(voterNum) >= twoThirds {
		return true
	}
	return false
}

// for committee
func LeaderMajorityTwoThird(voterNum, committeeSize int) bool {
	if (voterNum < 0) || (committeeSize < 1) {
		fmt.Println("MajorityTwoThird, inputs out of range")
		return false
	}
	// Examples
	// committeeSize= 1 twoThirds= 1
	// committeeSize= 2 twoThirds= 1
	// committeeSize= 3 twoThirds= 2
	// committeeSize= 4 twoThirds= 3
	// committeeSize= 5 twoThirds= 4
	// committeeSize= 6 twoThirds= 4
	var twoThirds float64
	if committeeSize == 2 {
		twoThirds = 1
	} else {
		twoThirds = math.Ceil(float64(committeeSize) * 2 / 3)
	}
	if float64(voterNum) >= twoThirds {
		return true
	}
	return false
}

//============================================================================
//============================================================================
// Testing support code
//============================================================================
//============================================================================
type Delegate1 struct {
	Name        string           `json:"name"`
	Address     string           `json:"address"`
	PubKey      string           `json:"pub_key"`
	VotingPower int64            `json:"voting_power"`
	NetAddr     types.NetAddress `json:"network_addr"`
}

func UserHomeDir() string {
	if runtime.GOOS == "windows" {
		home := os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		return home
	}
	return os.Getenv("HOME")
}

func configDelegates(dataDir string /*myPubKey ecdsa.PublicKey*/) []*types.Delegate {
	delegates1 := make([]*Delegate1, 0)

	// Hack for compile
	// TODO: move these hard-coded filepath to config
	filePath := path.Join(dataDir, "delegates.json")
	file, err := ioutil.ReadFile(filePath)
	if err != nil {
		fmt.Println("unable load delegate file", "error", err)
		fmt.Println("File is at", filePath /*config.DefaultDelegatePath*/)
	}
	err = cdc.UnmarshalJSON(file, &delegates1)
	if err != nil {
		fmt.Println("Unable unmarshal delegate file")
		fmt.Println(err)
	}

	delegates := make([]*types.Delegate, 0)
	for _, d := range delegates1 {
		pubKeyBytes, err := b64.StdEncoding.DecodeString(d.PubKey)
		pubKey, err := crypto.UnmarshalPubkey(pubKeyBytes)
		if err != nil {
			panic(fmt.Sprintf("read public key of delegate failed, %v", err))
		}

		addr, err := meter.ParseAddress(d.Address)
		if err != nil {
			panic(fmt.Sprintf("read address of delegate failed, %v", err))
		}
		dd := types.NewDelegate([]byte(d.Name), addr, *pubKey, d.VotingPower)
		dd.NetAddr = d.NetAddr
		delegates = append(delegates, dd)
	}
	return delegates
}

func (conR *ConsensusReactor) LoadBlockBytes(num uint32) []byte {
	raw, err := conR.chain.GetTrunkBlockRaw(num)
	if err != nil {
		fmt.Print("Error load raw block: ", err)
		return []byte{}
	}
	return raw[:]
}

func PrintDelegates(delegates []*types.Delegate) {
	fmt.Println("============================================")
	for i, dd := range delegates {
		keyBytes := crypto.FromECDSAPub(&dd.PubKey)
		pubKeyStr := base64.StdEncoding.EncodeToString(keyBytes)

		fmt.Printf("Delegate %d:\n Address:%s\n Public Key: %v\n Voting Power:%d, Network Address: %s:%d\n",
			i+1, dd.Address, pubKeyStr, dd.VotingPower, dd.NetAddr.IP.String(), dd.NetAddr.Port)
	}
	fmt.Println("============================================")
}

// entry point for each committee
// configSize is configured(expected) and normally minimumSize is committee size
func GetConsensusDelegates(dataDir string, configSize int, minimumSize int) []*types.Delegate {
	delegates, err := staking.GetInternalDelegateList()
	if err == nil && len(delegates) >= minimumSize {
		fmt.Println("delegatesList from staking", "delegates size", len(delegates))
		PrintDelegates(delegates)
	} else {
		delegates = configDelegates(dataDir)
		fmt.Println("delegatesList from configuration file e.g. delegates.json", "delegates size", len(delegates))
		PrintDelegates(delegates)
	}

	fmt.Println("Configured Delegates size", configSize, "Committee size", minimumSize)
	if len(delegates) >= configSize {
		return delegates[:configSize]
	} else {
		return delegates
	}
}

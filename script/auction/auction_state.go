package auction

import (
	"bytes"
	"encoding/gob"
	"math/big"

	"github.com/dfinlab/meter/meter"
	"github.com/dfinlab/meter/runtime/statedb"
	"github.com/dfinlab/meter/state"

	"github.com/ethereum/go-ethereum/common"
)

// the global variables in auction
var (
	// 0x74696f6e2d6163636f756e742d61646472657373
	AuctionAccountAddr = meter.BytesToAddress([]byte("auction-account-address"))
	SummaryListKey     = meter.Blake2b([]byte("summary-list-key"))
	AuctionCBKey       = meter.Blake2b([]byte("auction-active-cb-key"))
)

// Candidate List
func (a *Auction) GetAuctionCB(state *state.State) (result *AuctionCB) {
	state.DecodeStorage(AuctionAccountAddr, AuctionCBKey, func(raw []byte) error {
		// fmt.Println("Loaded Raw Hex: ", hex.EncodeToString(raw))
		decoder := gob.NewDecoder(bytes.NewBuffer(raw))
		var auctionCB AuctionCB
		err := decoder.Decode(&auctionCB)
		if err != nil {
			if err.Error() == "EOF" && len(raw) == 0 {
				// empty raw, do nothing
			} else {
				log.Warn("Error during decoding auctionCB, set it as an empty list", "err", err)
			}
			result = &AuctionCB{}
			return nil

		}
		result = &auctionCB
		return nil
	})
	return
}

func (a *Auction) SetAuctionCB(auctionCB *AuctionCB, state *state.State) {
	state.EncodeStorage(AuctionAccountAddr, AuctionCBKey, func() ([]byte, error) {
		buf := bytes.NewBuffer([]byte{})
		encoder := gob.NewEncoder(buf)
		err := encoder.Encode(auctionCB)
		return buf.Bytes(), err
	})
}

// summary List
func (a *Auction) GetSummaryList(state *state.State) (result *AuctionSummaryList) {
	state.DecodeStorage(AuctionAccountAddr, SummaryListKey, func(raw []byte) error {
		decoder := gob.NewDecoder(bytes.NewBuffer(raw))

		var summaries []*AuctionSummary
		err := decoder.Decode(&summaries)
		result = NewAuctionSummaryList(summaries)
		if err != nil {
			if err.Error() == "EOF" && len(raw) == 0 {
				// empty raw, do nothing
			} else {
				log.Warn("Error during decoding auctionSummary list", "err", err)
			}
			return nil
		}
		return nil
	})
	return
}

func (a *Auction) SetSummaryList(summaryList *AuctionSummaryList, state *state.State) {
	state.EncodeStorage(AuctionAccountAddr, SummaryListKey, func() ([]byte, error) {
		buf := bytes.NewBuffer([]byte{})
		encoder := gob.NewEncoder(buf)
		err := encoder.Encode(summaryList.Summaries)
		return buf.Bytes(), err
	})
}

//==================== account openation===========================
func (a *Auction) TransferMTRToAuction(addr meter.Address, amount *big.Int, state *state.State) error {
	if amount.Sign() == 0 {
		return nil
	}
	var balance *big.Int

	balance = state.GetEnergy(addr)
	state.SetEnergy(meter.Address(addr), new(big.Int).Sub(balance, amount))

	balance = state.GetEnergy(AuctionAccountAddr)
	state.SetEnergy(AuctionAccountAddr, new(big.Int).Add(balance, amount))
	return nil
}

func (a *Auction) SendMTRGToBidder(addr meter.Address, amount *big.Int, stateDB *statedb.StateDB) error {
	if amount.Sign() == 0 {
		return nil
	}

	// in auction, MeterGov is mint action.
	stateDB.MintBalance(common.Address(addr), amount)
	return nil
}

//==============================================
// when auction is over
func (a *Auction) ClearAuction(cb *AuctionCB, state *state.State) (*big.Int, *big.Int, error) {
	stateDB := statedb.New(state)

	actualPrice := big.NewInt(0)
	actualPrice = actualPrice.Div(cb.RcvdMTR, cb.RlsdMTRG)
	actualPrice = actualPrice.Mul(actualPrice, big.NewInt(1e18))
	if actualPrice.Cmp(cb.RsvdPrice) < 0 {
		actualPrice = cb.RsvdPrice
	}

	total := big.NewInt(0)
	for _, tx := range cb.AuctionTxs {
		mtrg := tx.Amount.Div(tx.Amount, actualPrice)
		a.SendMTRGToBidder(tx.Addr, mtrg, stateDB)
		total = total.Add(total, mtrg)
	}

	leftOver := big.NewInt(0)
	leftOver = leftOver.Sub(cb.RlsdMTRG, total)
	a.SendMTRGToBidder(AuctionAccountAddr, leftOver, stateDB)
	a.logger.Info("finished auctionCB clear...", "actualPrice", actualPrice.Uint64(), "leftOver", leftOver.Uint64())
	return actualPrice, leftOver, nil
}
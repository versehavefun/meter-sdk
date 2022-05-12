// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package genesis

import (
	"bytes"
	"math/big"
	"sort"
	"strconv"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/inconshreveable/log15"

	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/script/accountlock"
	"github.com/meterio/meter-pov/state"
)

var (
	log = log15.New("pkg", "genesis")
)

// "address", "meter amount", "mterGov amount", "memo", "release epoch"
var profiles [][5]string = [][5]string{

	// team accounts
	//{"0x671e86B2929688e2667E2dB56e0472B7a3AF6Ad6", "10", "3750000", "Team 1", "4380"},  // 182.5 days
	//{"0x3D63757898984ab66716A0F4aAF1A60eFc0608e1", "10", "3750000", "Team 2", "8760"},  // 365 days
	//{"0x6e4c7C6dB73371C049Ee2E9ac15557DceEbff4a0", "10", "3750000", "Team 3", "13140"}, // 547.5 days
	//{"0xdC7b7279ef4940a0776CA15d08ab5296a0ECBE96", "10", "3750000", "Team 4", "17520"}, // 730 days
	//{"0xFa1424A93C7cF926fFFACBb9858C480102585C24", "10", "3750000", "Team 5", "21900"}, // 912.5 days
	//{"0x826e9f61c8179Aca37fe81620B989125Ccb36089", "10", "3750000", "Team 6", "26280"}, // 1095 days
	//{"0x11A9E06994968b696bEE2f643fFdcAe7c0D5c060", "10", "3750000", "Team 7", "30660"}, // 1277.5 days
	//{"0x8E7896D70618D38651c7231d26A2ABee259216c0", "10", "3750000", "Team 8", "35050"}, // 1460 days

	// Foundation
	//{"0x61ad236FCcCF342B1b76a7DE5D0475EEeb8405a9", "10", "3000000", "Marketing", "2"}, // 1 day
	//{"0xAca2D120eE27e0E493bF91Ee9f3315Ec005b9CE3", "10", "5300000", "Foundation Ops", "24"},
	//{"0x8B9Ef3147950C00422cDED432DC5b4c0AA2D2Cdd", "10", "1700000", "Public Sale", "2"},
	//{"0x78BA7A9E73e219E85bE44D484529944355BF6701", "10", "30000000", "Foundation Lock", "17520"}, // 730 days

	// testnet meter mapping
	{"0xfB88393e18e1B8c45fC2a90b9c533C61D20E290c", "89672.78", "89672.78", "Account for DFL STPT", "2"}, // 1 day
	{"0xa6FfDc4f4de5D00f1a218d702a5283300Dfbd5f2", "88763.59", "88763.59", "Account for DFL Airdrop", "2"},
	
	{"0x78451Ed0FA6C3feb508E9Ac67Efc1f7Beb3e6f45", "100000", "100000", "Mathew Williams", "24"},
	{"0xF29696f3f9638ACEf52c63a32b36FcB171330A5E", "100000", "100000", "Glen Fanno", "24"},
	{"0x4944EC08f01896B46F562e8000D62b49E0A76B8F", "100000", "100000", "Melody Woods", "24"},
	{"0xcE5f771d97810174dC18E417dC5E565008A3aFB4", "100000", "100000", "Elizabeth", "24"},
	{"0x6E5690d4590DC2bcD740a600aE536Fa686c4Ee6e", "100000", "100000", "Mike", "24"},
	{"0x3ADD886D9A9D8EfaCcAecD7f3Eb19F8e6FfFdaa1", "100000", "100000", "Christine", "24"},
	{"0xf221DcCF796277E4C446DcDd3022AA863685E2f0", "100000", "100000", "Mosconi", "24"},
	{"0x079Dc86e6c737233bA248E3F1642b1923f89fe68", "100000", "100000", "Melissa Murray", "24"},
	{"0x02D8B960c4621E018Eb75Ac25e81F7145356DF90", "100000", "100000", "Donald Green", "24"},
	{"0x2647711D4FC77BD1Ff3012FD134cea7122AE2a55", "100000", "100000", "Roberta Gutierrez", "24"},
	
	{"0xfd746a652b3a3A81bAA01CB92faE5ba4C32c3667", "540.10", "0", "Tony Wang", "24"},
	{"0xf53E2Edf6d35c163e23F196faA49aB7181322d1e", "10", "10", "sdk Dong", "2"},
}

func LoadVestProfile() []*accountlock.Profile {
	plans := make([]*accountlock.Profile, 0, len(profiles))
	for _, p := range profiles {
		address := meter.MustParseAddress(p[0])
		mtr, err := strconv.ParseFloat(p[1], 64)
		if err != nil {
			log.Error("parse meter value failed", "error", err)
			continue
		}
		mtrg, err := strconv.ParseFloat(p[2], 64)
		if err != nil {
			log.Error("parse meterGov value failed", "error", err)
			continue
		}
		epoch, err := strconv.ParseUint(p[4], 10, 64)
		if err != nil {
			log.Error("parse release block epoch failed", "error", err)
			continue
		}
		memo := []byte(p[3])

		pp := accountlock.NewProfile(address, memo, 0, uint32(epoch), FloatToBigInt(mtr), FloatToBigInt(mtrg))
		log.Debug("new profile created", "profile", pp.ToString())

		plans = append(plans, pp)
	}

	sort.SliceStable(plans, func(i, j int) bool {
		return (bytes.Compare(plans[i].Addr.Bytes(), plans[j].Addr.Bytes()) <= 0)
	})

	return plans
}

func SetProfileList(lockList *accountlock.ProfileList, state *state.State) {
	state.EncodeStorage(accountlock.AccountLockAddr, accountlock.AccountLockProfileKey, func() ([]byte, error) {
		// buf := bytes.NewBuffer([]byte{})
		// encoder := gob.NewEncoder(buf)
		// err := encoder.Encode(lockList)
		// return buf.Bytes(), err
		return rlp.EncodeToBytes(lockList.Profiles)
	})
}

func SetAccountLockProfileState(list []*accountlock.Profile, state *state.State) {
	pList := accountlock.NewProfileList(list)
	SetProfileList(pList, state)
}

func FloatToBigInt(val float64) *big.Int {
	fval := float64(val * 1e09)
	bigval := big.NewInt(int64(fval))
	return bigval.Mul(bigval, big.NewInt(1e09))
}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package genesis

import (
	"math/big"

	"github.com/meterio/meter-pov/builtin"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/state"
	"github.com/meterio/meter-pov/tx"
	"github.com/meterio/meter-pov/vm"
)

// NewTestnet create genesis for testnet.
func NewTestnet() *Genesis {
	launchTime := uint64(1640738101) // 'Tue Jun 26 2018 20:00:00 GMT+0800 (CST)'

	// use this address as executor instead of builtin one, for test purpose
	executor, _ := meter.ParseAddress("0xB86EEDAD0EEB98D3B358E4E9C09424685280CDD8")
	acccount0, _ := meter.ParseAddress("0xCEDBC36FDB9C361710F0ECA5D766F78324C2975D")

	//master0, _ := meter.ParseAddress("0xbc675bf8f737faad6195d20917a57bb0f0ddb5f6")
	endorser0, _ := meter.ParseAddress("0xB9B64A9EB4C6A6E1C6C0C1070A282A6840DF6011")

	builder := new(Builder).
		Timestamp(launchTime).
		GasLimit(meter.InitialGasLimit).
		State(func(state *state.State) error {
			tokenSupply := new(big.Int)
			energySupply := new(big.Int)

			// alloc precompiled contracts
			for addr := range vm.PrecompiledContractsByzantium {
				state.SetCode(meter.Address(addr), emptyRuntimeBytecode)
			}

			// accountlock states
			profiles := LoadVestProfile()
			for _, p := range profiles {
				state.SetBalance(p.Addr, p.MeterGovAmount)
				tokenSupply.Add(tokenSupply, p.MeterGovAmount)

				state.SetEnergy(p.Addr, p.MeterAmount)
				energySupply.Add(energySupply, p.MeterAmount)
			}
			SetAccountLockProfileState(profiles, state)

			// setup builtin contracts
			state.SetCode(builtin.MeterTracker.Address, builtin.MeterTracker.RuntimeBytecodes()) // 0x0000000000004e65774d657465724e6174697665

			state.SetCode(builtin.Meter.Address, builtin.Meter.RuntimeBytecodes())         // 0x000000000000000000004d657465724552433230
			state.SetCode(builtin.MeterGov.Address, builtin.MeterGov.RuntimeBytecodes())   // 0x000000000000004d65746572476f764552433230
			state.SetCode(builtin.Executor.Address, builtin.Executor.RuntimeBytecodes())   // 0x72c49458728c5661f19bcab6da76d96ae94e6e83
			state.SetCode(builtin.Params.Address, builtin.Params.RuntimeBytecodes())       // 0x0000000000000000000000000000506172616d73
			state.SetCode(builtin.Prototype.Address, builtin.Prototype.RuntimeBytecodes()) // 0x000000000000000000000050726f746f74797065
			state.SetCode(builtin.Extension.Address, builtin.Extension.RuntimeBytecodes()) // 0x0000000000000000000000457874656e73696f6e

			//state.SetCode(builtin.OldMeter.Address, builtin.OldMeter.RuntimeBytecodes())               // 0x0000000000000000000000000000004d65746572
			//state.SetCode(builtin.OldMeterGov.Address, builtin.OldMeterGov.RuntimeBytecodes())         // 0x0000000000000000000000004d65746572476f76
			//state.SetCode(builtin.OldMeterTracker.Address, builtin.OldMeterTracker.RuntimeBytecodes()) // 0x0000000000000000004d657465724e6174697665

			// 50 billion for account0
			amount := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(50*1000*1000*1000))
			state.SetBalance(acccount0, amount)
			state.SetEnergy(acccount0, amount)

			tokenSupply.Add(tokenSupply, amount)
			energySupply.Add(energySupply, amount)

			// 25 million for endorser0
			amount = new(big.Int).Mul(big.NewInt(1e18), big.NewInt(25*1000*1000))
			state.SetBalance(endorser0, amount)
			state.SetEnergy(endorser0, amount)
			tokenSupply.Add(tokenSupply, amount)
			energySupply.Add(energySupply, amount)

			builtin.MeterTracker.Native(state).SetInitialSupply(tokenSupply, energySupply)
			return nil
		}).
		// set initial params
		// use an external account as executor to manage testnet easily
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyExecutorAddress, new(big.Int).SetBytes(executor[:]))),
			meter.Address{}).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyBaseGasPrice, meter.InitialBaseGasPrice)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyProposerEndorsement, meter.InitialProposerEndorsement)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyPowPoolCoef, meter.InitialPowPoolCoef)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyPowPoolCoefFadeDays, meter.InitialPowPoolCoefFadeDays)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyPowPoolCoefFadeRate, meter.InitialPowPoolCoefFadeRate)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyValidatorBenefitRatio, meter.InitialValidatorBenefitRatio)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyValidatorBaseReward, meter.InitialValidatorBaseReward)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyAuctionReservedPrice, meter.InitialAuctionReservedPrice)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyMinRequiredByDelegate, meter.InitialMinRequiredByDelegate)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyAuctionInitRelease, meter.InitialAuctionInitRelease)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyBorrowInterestRate, meter.InitialBorrowInterestRate)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyConsensusCommitteeSize, meter.InitialConsensusCommitteeSize)),
			executor).
		Call(
			tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyConsensusDelegateSize, meter.InitialConsensusDelegateSize)),
			executor).
		Call(tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyNativeMtrERC20Address, big.NewInt(0).SetBytes(builtin.Meter.Address.Bytes()))),
			executor).
		Call(tx.NewClause(&builtin.Params.Address).WithData(mustEncodeInput(builtin.Params.ABI, "set", meter.KeyNativeMtrgERC20Address, big.NewInt(0).SetBytes(builtin.MeterGov.Address.Bytes()))),
			executor)

	id, err := builder.ComputeID()
	if err != nil {
		panic(err)
	}
	return &Genesis{builder, id, "testnet"}
}

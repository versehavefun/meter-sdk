// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package preset

//go:generate go-bindata -nometadata -ignore=.DS_Store -pkg preset -o bindata.go mainnet/... testnet/...
// or
//go:generate go-bindata -pkg=preset testnet mainnet

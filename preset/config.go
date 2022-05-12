// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package preset

import (
	"fmt"
)

// The initial version of main network is Edison.
type PresetConfig struct {
	CommitteeMinSize int
	CommitteeMaxSize int
	DelegateMaxSize  int
	DiscoServer      string
	DiscoTopic       string
}

var (
	MainPresetConfig = &PresetConfig{
		CommitteeMinSize: 2,
		CommitteeMaxSize: 300,
		DelegateMaxSize:  300,
		DiscoServer:      "enode://30fa4a57e203cfef6eb13b2fec75e17849fb9e0be41f7abfc5992955b8c86e4ef484f27efe0d6250ec95a4a871be4b8151727dc86f33d3acfeb92b394e702cbd@13.214.56.167:55555",
		DiscoTopic:       "mainnet",
	}

	TestPresetConfig = &PresetConfig{
		CommitteeMinSize: 2,
		CommitteeMaxSize: 300,
		DelegateMaxSize:  500,
		DiscoServer:      "enode://f472ba62f3acdf9e22c356e62e2af977d24815aa8eb493dd63f137384100d6bab629fe3e600aa97c669572669e57bf27433954f4d24329d72d801bc543a8732f@13.213.3.39:55555",
		DiscoTopic:       "testnet",
	}
)

func (p *PresetConfig) ToString() string {
	return fmt.Sprintf("CommitteeMaxSize: %v DelegateMaxSize: %v DiscoServer: %v : DiscoTopic%v",
		p.CommitteeMinSize, p.CommitteeMaxSize, p.DelegateMaxSize, p.DiscoServer, p.DiscoTopic)
}

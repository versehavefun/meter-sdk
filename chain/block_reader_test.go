// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package chain_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBlockReader(t *testing.T) {
	ch := initChain()
	b0 := ch.GenesisBlock()

	b1 := newBlock(b0, 2)
	ch.AddBlock(b1, nil, true)

	b2 := newBlock(b1, 2)
	ch.AddBlock(b2, nil, true)

	b3 := newBlock(b2, 2)
	ch.AddBlock(b3, nil, true)

	b4 := newBlock(b3, 2)
	ch.AddBlock(b4, nil, true)

	br := ch.NewBlockReader(b2.Header().ID())

	blks, err := br.Read()
	assert.Nil(t, err)
	assert.Equal(t, blks[0].Header().ID(), b3.Header().ID())
	assert.False(t, blks[0].Obsolete)

	blks, err = br.Read()
	assert.Nil(t, err)
	assert.Equal(t, blks[0].Header().ID(), b4.Header().ID())
	assert.False(t, blks[0].Obsolete)
}

func TestBlockReaderFork(t *testing.T) {
	ch := initChain()
	b0 := ch.GenesisBlock()

	b1 := newBlock(b0, 1)
	ch.AddBlock(b1, nil, true)

	b2 := newBlock(b1, 2)
	ch.AddBlock(b2, nil, true)

	b2x := newBlock(b1, 2)
	ch.AddBlock(b2x, nil, true)

	b3 := newBlock(b2, 3)
	ch.AddBlock(b3, nil, true)

	b4 := newBlock(b3, 4)
	ch.AddBlock(b4, nil, true)

	br := ch.NewBlockReader(b2x.Header().ID())

	blks, err := br.Read()
	assert.Nil(t, err)
	fmt.Println("blocks:", blks)

	assert.Equal(t, len(blks), 1)
	assert.Equal(t, blks[0].Header().ID(), b3.Header().ID())
	assert.False(t, blks[0].Obsolete)
	// assert.Equal(t, blks[0].Header().ID(), b2.Header().ID())
	// assert.False(t, blks[0].Obsolete)

	blks, err = br.Read()
	fmt.Println("blocks 2:", blks)
	assert.Nil(t, err)
	assert.Equal(t, blks[0].Header().ID(), b4.Header().ID())
	assert.False(t, blks[0].Obsolete)
}

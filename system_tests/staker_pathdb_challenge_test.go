// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

// race detection makes things slow and miss timeouts
//go:build challengetest

package arbtest

import "testing"

func TestChallengeStakersPathDBFaultyHonestActive(t *testing.T) {
	stakerPathDBTestImpl(t, true, false)
}

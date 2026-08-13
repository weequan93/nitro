package blacklist

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/offchainlabs/nitro/arbos/addressSet"
	"github.com/offchainlabs/nitro/arbos/storage"
)

var (
	RemoveBlacklistTxFromSelector = [4]byte{0xda, 0xe8, 0x43, 0x49}
	RemoveBlacklistTxToSelector   = [4]byte{0x89, 0xe2, 0x5c, 0x2a}
	emptyAddressPadding           [12]byte
)

func IsEmergencyRemovalInput(input []byte) bool {
	return len(input) == 4+32 &&
		bytes.Equal(input[4:16], emptyAddressPadding[:]) &&
		(bytes.Equal(input[:4], RemoveBlacklistTxFromSelector[:]) || bytes.Equal(input[:4], RemoveBlacklistTxToSelector[:]))
}

type Blacklist struct {
	storage        *storage.Storage
	blackListOwner *addressSet.AddressSet
	txFromAddrs    *addressSet.AddressSet
	txToAddrs      *addressSet.AddressSet
}

type SubspaceID []byte

var (
	blackListOwnerOffSet SubspaceID = []byte{0}
	txFromAddrsSubspace  SubspaceID = []byte{1}
	txToAddrsSubspace    SubspaceID = []byte{2}
)

func InitializeBlacklist(sto *storage.Storage) error {
	_ = addressSet.Initialize(sto.OpenSubStorage(blackListOwnerOffSet))
	_ = addressSet.Initialize(sto.OpenSubStorage(txFromAddrsSubspace))
	return addressSet.Initialize(sto.OpenSubStorage(txToAddrsSubspace))
}

func OpenBlacklist(sto *storage.Storage) *Blacklist {
	return &Blacklist{
		sto,
		addressSet.OpenAddressSet(sto.OpenSubStorage(blackListOwnerOffSet)),
		addressSet.OpenAddressSet(sto.OpenSubStorage(txFromAddrsSubspace)),
		addressSet.OpenAddressSet(sto.OpenSubStorage(txToAddrsSubspace)),
	}
}

func (blacklist *Blacklist) BlacklistOwner() *addressSet.AddressSet {
	return blacklist.blackListOwner
}

func (blacklist *Blacklist) TxFromAddrs() *addressSet.AddressSet {
	return blacklist.txFromAddrs
}

func (blacklist *Blacklist) TxToAddrs() *addressSet.AddressSet {
	return blacklist.txToAddrs
}

// IsQuarantinedFree applies the DeriwOS union rule: membership in either
// legacy direction list quarantines the address in every execution role.
// The caller is responsible for charging two deterministic storage reads.
func (blacklist *Blacklist) IsQuarantinedFree(addr common.Address) bool {
	fromMember := blacklist.txFromAddrs.IsMemberFree(addr)
	toMember := blacklist.txToAddrs.IsMemberFree(addr)
	return fromMember || toMember
}

func (blacklist *Blacklist) IsBlacklistTxCheck(from *common.Address, tx *types.Transaction) bool {

	if tx != nil && tx.To() != nil {
		addr := common.HexToAddress(tx.To().String())
		isBlacklistToContract, err := blacklist.TxToAddrs().IsMember(addr)
		if err != nil {
			return false
		}

		if isBlacklistToContract == true {
			return true
		}
	}

	if from != nil {
		//addr := common.HexToAddress(from.String())
		isBlacklistFromContract, err := blacklist.TxFromAddrs().IsMember(*from)
		if err != nil {
			return false
		}

		if isBlacklistFromContract == true {
			return true
		}

	}
	return false
}

func (blacklist *Blacklist) IsBlacklistAddrCheck(addr *common.Address) bool {
	if addr == nil {
		return false
	}
	isBlacklistToAddress, err := blacklist.TxToAddrs().IsMember(*addr)
	if err != nil {
		return false
	}

	if isBlacklistToAddress == true {
		return true
	}

	isBlacklistFromAddress, err := blacklist.TxFromAddrs().IsMember(*addr)
	if err != nil {
		return false
	}

	if isBlacklistFromAddress == true {
		return true
	}

	return false
}

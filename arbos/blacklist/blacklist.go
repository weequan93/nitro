package blacklist

import (
	"bytes"
	"errors"

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
	banFlagsSubspace     SubspaceID = []byte{3}
)

const (
	// BanFlagAll is the only flag enforced by the general transaction blacklist.
	BanFlagAll uint64 = 1
	// BanFlagERC20Transfer records ERC20/USDT transfer bans only. DeriwOS
	// does not enforce this flag or inspect token transfer calldata.
	BanFlagERC20Transfer uint64 = 2
)

var ErrInvalidBanFlag = errors.New("unsupported blacklist ban flag")

// TxFromAddrsWithFlag returns a filtered view of the sender list, not a
// separate stored list. Both directions share one stored ban flag per address.
func (blacklist *Blacklist) TxFromAddrsWithFlag(flag uint64) (*FlaggedAddressSet, error) {
	return blacklist.addrsWithFlag(flag, blacklist.txFromAddrs)
}

func (blacklist *Blacklist) TxToAddrsWithFlag(flag uint64) (*FlaggedAddressSet, error) {
	return blacklist.addrsWithFlag(flag, blacklist.txToAddrs)
}

func validateBanFlag(flag uint64) error {
	switch flag {
	case BanFlagAll, BanFlagERC20Transfer:
		return nil
	default:
		return ErrInvalidBanFlag
	}
}

func (blacklist *Blacklist) addrsWithFlag(flag uint64, addresses *addressSet.AddressSet) (*FlaggedAddressSet, error) {
	if err := validateBanFlag(flag); err != nil {
		return nil, err
	}
	return &FlaggedAddressSet{blacklist: blacklist, addresses: addresses, flag: flag}, nil
}

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

// LegacyBanTypeFree preserves the pre-DeriwOS-6 interpretation and two-read
// consensus gas cost. Every historical blacklist member has BanFlagAll.
func (blacklist *Blacklist) LegacyBanTypeFree(addr common.Address) uint64 {
	fromMember := blacklist.txFromAddrs.IsMemberFree(addr)
	toMember := blacklist.txToAddrs.IsMemberFree(addr)
	if fromMember || toMember {
		return BanFlagAll
	}
	return 0
}

// BanTypeFree returns the same flag as BanType without charging storage gas
// through the burner. Callers accounting for consensus gas must charge three
// reads per checked address: both direction memberships and the stored flag.
func (blacklist *Blacklist) BanTypeFree(addr common.Address) uint64 {
	fromMember := blacklist.txFromAddrs.IsMemberFree(addr)
	toMember := blacklist.txToAddrs.IsMemberFree(addr)
	flag := blacklist.banFlags().GetFree(common.BytesToHash(addr.Bytes())).Big().Uint64()
	return effectiveBanFlag(fromMember || toMember, flag)
}

func (blacklist *Blacklist) IsBlacklistTxCheck(from *common.Address, tx *types.Transaction) bool {
	if tx != nil && tx.To() != nil {
		flag, err := blacklist.directionBanType(blacklist.txToAddrs, *tx.To())
		if err != nil {
			return false
		}
		if flag == BanFlagAll {
			return true
		}
	}
	if from != nil {
		flag, err := blacklist.directionBanType(blacklist.txFromAddrs, *from)
		if err != nil {
			return false
		}
		if flag == BanFlagAll {
			return true
		}
	}
	return false
}

func (blacklist *Blacklist) IsBlacklistAddrCheck(addr *common.Address) bool {
	if addr == nil {
		return false
	}
	flag, err := blacklist.BanType(*addr)
	return err == nil && flag == BanFlagAll
}

func (blacklist *Blacklist) banFlags() *storage.Storage {
	return blacklist.storage.OpenSubStorage(banFlagsSubspace)
}

// effectiveBanFlag preserves historical entries that predate flag storage.
// Zero is never accepted by flag setters; an unflagged existing entry means all.
func effectiveBanFlag(member bool, storedFlag uint64) uint64 {
	if !member {
		return 0
	}
	if storedFlag == 0 {
		return BanFlagAll
	}
	return storedFlag
}

// SetBanType writes one shared flag without moving or duplicating addresses.
// The caller adds the requested direction in the same StateDB transaction.
func (blacklist *Blacklist) SetBanType(addr common.Address, flag uint64) error {
	if err := validateBanFlag(flag); err != nil {
		return err
	}
	return blacklist.banFlags().SetUint64(common.BytesToHash(addr.Bytes()), flag)
}

// BanType returns the stored flag for an address in either direction list,
// BanFlagAll for historical unflagged entries, or zero for an unlisted address.
// Each of its three storage reads charges gas through the burner and can fail
// (for example, when the calling precompile runs out of gas).
func (blacklist *Blacklist) BanType(addr common.Address) (uint64, error) {
	fromMember, err := blacklist.txFromAddrs.IsMember(addr)
	if err != nil {
		return 0, err
	}
	toMember, err := blacklist.txToAddrs.IsMember(addr)
	if err != nil {
		return 0, err
	}
	flag, err := blacklist.banFlags().GetUint64(common.BytesToHash(addr.Bytes()))
	if err != nil {
		return 0, err
	}
	return effectiveBanFlag(fromMember || toMember, flag), nil
}

func (blacklist *Blacklist) directionBanType(addresses *addressSet.AddressSet, addr common.Address) (uint64, error) {
	member, err := addresses.IsMember(addr)
	if err != nil || !member {
		return 0, err
	}
	flag, err := blacklist.banFlags().GetUint64(common.BytesToHash(addr.Bytes()))
	if err != nil {
		return 0, err
	}
	return effectiveBanFlag(member, flag), nil
}

// ClearBanTypeIfUnlisted prevents an old flag being reused after final removal.
func (blacklist *Blacklist) ClearBanTypeIfUnlisted(addr common.Address) error {
	fromMember, err := blacklist.txFromAddrs.IsMember(addr)
	if err != nil {
		return err
	}
	toMember, err := blacklist.txToAddrs.IsMember(addr)
	if err != nil {
		return err
	}
	if fromMember || toMember {
		return nil
	}
	return blacklist.banFlags().Clear(common.BytesToHash(addr.Bytes()))
}

// FlaggedAddressSet is a view over a direction list and its shared ban flags.
// It stores no separate address set for a type.
type FlaggedAddressSet struct {
	blacklist *Blacklist
	addresses *addressSet.AddressSet
	flag      uint64
}

func (set *FlaggedAddressSet) IsMember(addr common.Address) (bool, error) {
	flag, err := set.blacklist.directionBanType(set.addresses, addr)
	return flag == set.flag, err
}

// AllMembers filters at most maxNumToScan entries from the underlying list.
func (set *FlaggedAddressSet) AllMembers(maxNumToScan uint64) ([]common.Address, error) {
	members, err := set.addresses.AllMembers(maxNumToScan)
	if err != nil {
		return nil, err
	}
	matching := make([]common.Address, 0, len(members))
	for _, addr := range members {
		flag, err := set.blacklist.banFlags().GetUint64(common.BytesToHash(addr.Bytes()))
		if err != nil {
			return nil, err
		}
		if effectiveBanFlag(true, flag) == set.flag {
			matching = append(matching, addr)
		}
	}
	return matching, nil
}

func (set *FlaggedAddressSet) Add(addr common.Address) error {
	if err := set.addresses.Add(addr); err != nil {
		return err
	}
	return set.blacklist.SetBanType(addr, set.flag)
}

func (set *FlaggedAddressSet) Remove(addr common.Address, arbosVersion uint64) error {
	member, err := set.IsMember(addr)
	if err != nil {
		return err
	}
	if !member {
		return errors.New("tried to remove absent blacklist ban flag")
	}
	if err := set.addresses.Remove(addr, arbosVersion); err != nil {
		return err
	}
	return set.blacklist.ClearBanTypeIfUnlisted(addr)
}

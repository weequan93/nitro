// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package staker

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
)

// StakeAmountReaderMetaData contains all meta data concerning the StakeAmountReader contract.
var StakeAmountReaderMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"_operator\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"isAdd\",\"type\":\"bool\"}],\"name\":\"SetOperator\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"_amount\",\"type\":\"uint256\"}],\"name\":\"SetAmount\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"defaultMinStakeAmount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"}],\"name\":\"getOperator\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"getOperatorsContains\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getOperatorsLength\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getStakeAmount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"minStakeAmount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"_amount\",\"type\":\"uint256\"}],\"name\":\"setDefaultMinStakeAmount\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_address\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"_amount\",\"type\":\"uint256\"}],\"name\":\"setMinStakeAmount\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_operator\",\"type\":\"address\"},{\"internalType\":\"bool\",\"name\":\"isAdd\",\"type\":\"bool\"}],\"name\":\"setOperator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"stakeAmount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// StakeAmountReaderABI is the input ABI used to generate the binding from.
// Deprecated: Use StakeAmountReaderMetaData.ABI instead.
var StakeAmountReaderABI = StakeAmountReaderMetaData.ABI

// StakeAmountReader is an auto generated Go binding around an Ethereum contract.
type StakeAmountReader struct {
	StakeAmountReaderCaller     // Read-only binding to the contract
	StakeAmountReaderTransactor // Write-only binding to the contract
	StakeAmountReaderFilterer   // Log filterer for contract events
}

// StakeAmountReaderCaller is an auto generated read-only Go binding around an Ethereum contract.
type StakeAmountReaderCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// StakeAmountReaderTransactor is an auto generated write-only Go binding around an Ethereum contract.
type StakeAmountReaderTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// StakeAmountReaderFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type StakeAmountReaderFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// StakeAmountReaderSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type StakeAmountReaderSession struct {
	Contract     *StakeAmountReader // Generic contract binding to set the session for
	CallOpts     bind.CallOpts      // Call options to use throughout this session
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// StakeAmountReaderCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type StakeAmountReaderCallerSession struct {
	Contract *StakeAmountReaderCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts            // Call options to use throughout this session
}

// StakeAmountReaderTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type StakeAmountReaderTransactorSession struct {
	Contract     *StakeAmountReaderTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts            // Transaction auth options to use throughout this session
}

// StakeAmountReaderRaw is an auto generated low-level Go binding around an Ethereum contract.
type StakeAmountReaderRaw struct {
	Contract *StakeAmountReader // Generic contract binding to access the raw methods on
}

// StakeAmountReaderCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type StakeAmountReaderCallerRaw struct {
	Contract *StakeAmountReaderCaller // Generic read-only contract binding to access the raw methods on
}

// StakeAmountReaderTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type StakeAmountReaderTransactorRaw struct {
	Contract *StakeAmountReaderTransactor // Generic write-only contract binding to access the raw methods on
}

// NewStakeAmountReader creates a new instance of StakeAmountReader, bound to a specific deployed contract.
func NewStakeAmountReader(address common.Address, backend bind.ContractBackend) (*StakeAmountReader, error) {
	contract, err := bindStakeAmountReader(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &StakeAmountReader{StakeAmountReaderCaller: StakeAmountReaderCaller{contract: contract}, StakeAmountReaderTransactor: StakeAmountReaderTransactor{contract: contract}, StakeAmountReaderFilterer: StakeAmountReaderFilterer{contract: contract}}, nil
}

// NewStakeAmountReaderCaller creates a new read-only instance of StakeAmountReader, bound to a specific deployed contract.
func NewStakeAmountReaderCaller(address common.Address, caller bind.ContractCaller) (*StakeAmountReaderCaller, error) {
	contract, err := bindStakeAmountReader(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &StakeAmountReaderCaller{contract: contract}, nil
}

// NewStakeAmountReaderTransactor creates a new write-only instance of StakeAmountReader, bound to a specific deployed contract.
func NewStakeAmountReaderTransactor(address common.Address, transactor bind.ContractTransactor) (*StakeAmountReaderTransactor, error) {
	contract, err := bindStakeAmountReader(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &StakeAmountReaderTransactor{contract: contract}, nil
}

// NewStakeAmountReaderFilterer creates a new log filterer instance of StakeAmountReader, bound to a specific deployed contract.
func NewStakeAmountReaderFilterer(address common.Address, filterer bind.ContractFilterer) (*StakeAmountReaderFilterer, error) {
	contract, err := bindStakeAmountReader(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &StakeAmountReaderFilterer{contract: contract}, nil
}

// bindStakeAmountReader binds a generic wrapper to an already deployed contract.
func bindStakeAmountReader(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(StakeAmountReaderABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_StakeAmountReader *StakeAmountReaderRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _StakeAmountReader.Contract.StakeAmountReaderCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_StakeAmountReader *StakeAmountReaderRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.StakeAmountReaderTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_StakeAmountReader *StakeAmountReaderRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.StakeAmountReaderTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_StakeAmountReader *StakeAmountReaderCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _StakeAmountReader.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_StakeAmountReader *StakeAmountReaderTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_StakeAmountReader *StakeAmountReaderTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.contract.Transact(opts, method, params...)
}

// DefaultMinStakeAmount is a free data retrieval call binding the contract method 0x3973688f.
//
// Solidity: function defaultMinStakeAmount() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCaller) DefaultMinStakeAmount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _StakeAmountReader.contract.Call(opts, &out, "defaultMinStakeAmount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// DefaultMinStakeAmount is a free data retrieval call binding the contract method 0x3973688f.
//
// Solidity: function defaultMinStakeAmount() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderSession) DefaultMinStakeAmount() (*big.Int, error) {
	return _StakeAmountReader.Contract.DefaultMinStakeAmount(&_StakeAmountReader.CallOpts)
}

// DefaultMinStakeAmount is a free data retrieval call binding the contract method 0x3973688f.
//
// Solidity: function defaultMinStakeAmount() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCallerSession) DefaultMinStakeAmount() (*big.Int, error) {
	return _StakeAmountReader.Contract.DefaultMinStakeAmount(&_StakeAmountReader.CallOpts)
}

// GetOperator is a free data retrieval call binding the contract method 0x05f63c8a.
//
// Solidity: function getOperator(uint256 index) view returns(address)
func (_StakeAmountReader *StakeAmountReaderCaller) GetOperator(opts *bind.CallOpts, index *big.Int) (common.Address, error) {
	var out []interface{}
	err := _StakeAmountReader.contract.Call(opts, &out, "getOperator", index)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// GetOperator is a free data retrieval call binding the contract method 0x05f63c8a.
//
// Solidity: function getOperator(uint256 index) view returns(address)
func (_StakeAmountReader *StakeAmountReaderSession) GetOperator(index *big.Int) (common.Address, error) {
	return _StakeAmountReader.Contract.GetOperator(&_StakeAmountReader.CallOpts, index)
}

// GetOperator is a free data retrieval call binding the contract method 0x05f63c8a.
//
// Solidity: function getOperator(uint256 index) view returns(address)
func (_StakeAmountReader *StakeAmountReaderCallerSession) GetOperator(index *big.Int) (common.Address, error) {
	return _StakeAmountReader.Contract.GetOperator(&_StakeAmountReader.CallOpts, index)
}

// GetOperatorsContains is a free data retrieval call binding the contract method 0xb6a0773b.
//
// Solidity: function getOperatorsContains(address account) view returns(bool)
func (_StakeAmountReader *StakeAmountReaderCaller) GetOperatorsContains(opts *bind.CallOpts, account common.Address) (bool, error) {
	var out []interface{}
	err := _StakeAmountReader.contract.Call(opts, &out, "getOperatorsContains", account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// GetOperatorsContains is a free data retrieval call binding the contract method 0xb6a0773b.
//
// Solidity: function getOperatorsContains(address account) view returns(bool)
func (_StakeAmountReader *StakeAmountReaderSession) GetOperatorsContains(account common.Address) (bool, error) {
	return _StakeAmountReader.Contract.GetOperatorsContains(&_StakeAmountReader.CallOpts, account)
}

// GetOperatorsContains is a free data retrieval call binding the contract method 0xb6a0773b.
//
// Solidity: function getOperatorsContains(address account) view returns(bool)
func (_StakeAmountReader *StakeAmountReaderCallerSession) GetOperatorsContains(account common.Address) (bool, error) {
	return _StakeAmountReader.Contract.GetOperatorsContains(&_StakeAmountReader.CallOpts, account)
}

// GetOperatorsLength is a free data retrieval call binding the contract method 0xba24ecab.
//
// Solidity: function getOperatorsLength() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCaller) GetOperatorsLength(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _StakeAmountReader.contract.Call(opts, &out, "getOperatorsLength")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetOperatorsLength is a free data retrieval call binding the contract method 0xba24ecab.
//
// Solidity: function getOperatorsLength() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderSession) GetOperatorsLength() (*big.Int, error) {
	return _StakeAmountReader.Contract.GetOperatorsLength(&_StakeAmountReader.CallOpts)
}

// GetOperatorsLength is a free data retrieval call binding the contract method 0xba24ecab.
//
// Solidity: function getOperatorsLength() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCallerSession) GetOperatorsLength() (*big.Int, error) {
	return _StakeAmountReader.Contract.GetOperatorsLength(&_StakeAmountReader.CallOpts)
}

// GetStakeAmount is a free data retrieval call binding the contract method 0x722580b6.
//
// Solidity: function getStakeAmount() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCaller) GetStakeAmount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _StakeAmountReader.contract.Call(opts, &out, "getStakeAmount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetStakeAmount is a free data retrieval call binding the contract method 0x722580b6.
//
// Solidity: function getStakeAmount() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderSession) GetStakeAmount() (*big.Int, error) {
	return _StakeAmountReader.Contract.GetStakeAmount(&_StakeAmountReader.CallOpts)
}

// GetStakeAmount is a free data retrieval call binding the contract method 0x722580b6.
//
// Solidity: function getStakeAmount() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCallerSession) GetStakeAmount() (*big.Int, error) {
	return _StakeAmountReader.Contract.GetStakeAmount(&_StakeAmountReader.CallOpts)
}

// MinStakeAmount is a free data retrieval call binding the contract method 0x95d322b0.
//
// Solidity: function minStakeAmount(address ) view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCaller) MinStakeAmount(opts *bind.CallOpts, arg0 common.Address) (*big.Int, error) {
	var out []interface{}
	err := _StakeAmountReader.contract.Call(opts, &out, "minStakeAmount", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MinStakeAmount is a free data retrieval call binding the contract method 0x95d322b0.
//
// Solidity: function minStakeAmount(address ) view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderSession) MinStakeAmount(arg0 common.Address) (*big.Int, error) {
	return _StakeAmountReader.Contract.MinStakeAmount(&_StakeAmountReader.CallOpts, arg0)
}

// MinStakeAmount is a free data retrieval call binding the contract method 0x95d322b0.
//
// Solidity: function minStakeAmount(address ) view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCallerSession) MinStakeAmount(arg0 common.Address) (*big.Int, error) {
	return _StakeAmountReader.Contract.MinStakeAmount(&_StakeAmountReader.CallOpts, arg0)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_StakeAmountReader *StakeAmountReaderCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _StakeAmountReader.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_StakeAmountReader *StakeAmountReaderSession) Owner() (common.Address, error) {
	return _StakeAmountReader.Contract.Owner(&_StakeAmountReader.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_StakeAmountReader *StakeAmountReaderCallerSession) Owner() (common.Address, error) {
	return _StakeAmountReader.Contract.Owner(&_StakeAmountReader.CallOpts)
}

// StakeAmount is a free data retrieval call binding the contract method 0xbf135267.
//
// Solidity: function stakeAmount(address ) view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCaller) StakeAmount(opts *bind.CallOpts, arg0 common.Address) (*big.Int, error) {
	var out []interface{}
	err := _StakeAmountReader.contract.Call(opts, &out, "stakeAmount", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// StakeAmount is a free data retrieval call binding the contract method 0xbf135267.
//
// Solidity: function stakeAmount(address ) view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderSession) StakeAmount(arg0 common.Address) (*big.Int, error) {
	return _StakeAmountReader.Contract.StakeAmount(&_StakeAmountReader.CallOpts, arg0)
}

// StakeAmount is a free data retrieval call binding the contract method 0xbf135267.
//
// Solidity: function stakeAmount(address ) view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCallerSession) StakeAmount(arg0 common.Address) (*big.Int, error) {
	return _StakeAmountReader.Contract.StakeAmount(&_StakeAmountReader.CallOpts, arg0)
}

// SetAmount is a paid mutator transaction binding the contract method 0xbea27193.
//
// Solidity: function SetAmount(uint256 _amount) returns()
func (_StakeAmountReader *StakeAmountReaderTransactor) SetAmount(opts *bind.TransactOpts, _amount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.contract.Transact(opts, "SetAmount", _amount)
}

// SetAmount is a paid mutator transaction binding the contract method 0xbea27193.
//
// Solidity: function SetAmount(uint256 _amount) returns()
func (_StakeAmountReader *StakeAmountReaderSession) SetAmount(_amount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.SetAmount(&_StakeAmountReader.TransactOpts, _amount)
}

// SetAmount is a paid mutator transaction binding the contract method 0xbea27193.
//
// Solidity: function SetAmount(uint256 _amount) returns()
func (_StakeAmountReader *StakeAmountReaderTransactorSession) SetAmount(_amount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.SetAmount(&_StakeAmountReader.TransactOpts, _amount)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_StakeAmountReader *StakeAmountReaderTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _StakeAmountReader.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_StakeAmountReader *StakeAmountReaderSession) RenounceOwnership() (*types.Transaction, error) {
	return _StakeAmountReader.Contract.RenounceOwnership(&_StakeAmountReader.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_StakeAmountReader *StakeAmountReaderTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _StakeAmountReader.Contract.RenounceOwnership(&_StakeAmountReader.TransactOpts)
}

// SetDefaultMinStakeAmount is a paid mutator transaction binding the contract method 0x53612ebe.
//
// Solidity: function setDefaultMinStakeAmount(uint256 _amount) returns()
func (_StakeAmountReader *StakeAmountReaderTransactor) SetDefaultMinStakeAmount(opts *bind.TransactOpts, _amount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.contract.Transact(opts, "setDefaultMinStakeAmount", _amount)
}

// SetDefaultMinStakeAmount is a paid mutator transaction binding the contract method 0x53612ebe.
//
// Solidity: function setDefaultMinStakeAmount(uint256 _amount) returns()
func (_StakeAmountReader *StakeAmountReaderSession) SetDefaultMinStakeAmount(_amount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.SetDefaultMinStakeAmount(&_StakeAmountReader.TransactOpts, _amount)
}

// SetDefaultMinStakeAmount is a paid mutator transaction binding the contract method 0x53612ebe.
//
// Solidity: function setDefaultMinStakeAmount(uint256 _amount) returns()
func (_StakeAmountReader *StakeAmountReaderTransactorSession) SetDefaultMinStakeAmount(_amount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.SetDefaultMinStakeAmount(&_StakeAmountReader.TransactOpts, _amount)
}

// SetMinStakeAmount is a paid mutator transaction binding the contract method 0xcb314fab.
//
// Solidity: function setMinStakeAmount(address _address, uint256 _amount) returns()
func (_StakeAmountReader *StakeAmountReaderTransactor) SetMinStakeAmount(opts *bind.TransactOpts, _address common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.contract.Transact(opts, "setMinStakeAmount", _address, _amount)
}

// SetMinStakeAmount is a paid mutator transaction binding the contract method 0xcb314fab.
//
// Solidity: function setMinStakeAmount(address _address, uint256 _amount) returns()
func (_StakeAmountReader *StakeAmountReaderSession) SetMinStakeAmount(_address common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.SetMinStakeAmount(&_StakeAmountReader.TransactOpts, _address, _amount)
}

// SetMinStakeAmount is a paid mutator transaction binding the contract method 0xcb314fab.
//
// Solidity: function setMinStakeAmount(address _address, uint256 _amount) returns()
func (_StakeAmountReader *StakeAmountReaderTransactorSession) SetMinStakeAmount(_address common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.SetMinStakeAmount(&_StakeAmountReader.TransactOpts, _address, _amount)
}

// SetOperator is a paid mutator transaction binding the contract method 0x558a7297.
//
// Solidity: function setOperator(address _operator, bool isAdd) returns()
func (_StakeAmountReader *StakeAmountReaderTransactor) SetOperator(opts *bind.TransactOpts, _operator common.Address, isAdd bool) (*types.Transaction, error) {
	return _StakeAmountReader.contract.Transact(opts, "setOperator", _operator, isAdd)
}

// SetOperator is a paid mutator transaction binding the contract method 0x558a7297.
//
// Solidity: function setOperator(address _operator, bool isAdd) returns()
func (_StakeAmountReader *StakeAmountReaderSession) SetOperator(_operator common.Address, isAdd bool) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.SetOperator(&_StakeAmountReader.TransactOpts, _operator, isAdd)
}

// SetOperator is a paid mutator transaction binding the contract method 0x558a7297.
//
// Solidity: function setOperator(address _operator, bool isAdd) returns()
func (_StakeAmountReader *StakeAmountReaderTransactorSession) SetOperator(_operator common.Address, isAdd bool) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.SetOperator(&_StakeAmountReader.TransactOpts, _operator, isAdd)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_StakeAmountReader *StakeAmountReaderTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _StakeAmountReader.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_StakeAmountReader *StakeAmountReaderSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.TransferOwnership(&_StakeAmountReader.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_StakeAmountReader *StakeAmountReaderTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.TransferOwnership(&_StakeAmountReader.TransactOpts, newOwner)
}

// StakeAmountReaderOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the StakeAmountReader contract.
type StakeAmountReaderOwnershipTransferredIterator struct {
	Event *StakeAmountReaderOwnershipTransferred // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *StakeAmountReaderOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(StakeAmountReaderOwnershipTransferred)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(StakeAmountReaderOwnershipTransferred)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *StakeAmountReaderOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *StakeAmountReaderOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// StakeAmountReaderOwnershipTransferred represents a OwnershipTransferred event raised by the StakeAmountReader contract.
type StakeAmountReaderOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_StakeAmountReader *StakeAmountReaderFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*StakeAmountReaderOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _StakeAmountReader.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &StakeAmountReaderOwnershipTransferredIterator{contract: _StakeAmountReader.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_StakeAmountReader *StakeAmountReaderFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *StakeAmountReaderOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _StakeAmountReader.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(StakeAmountReaderOwnershipTransferred)
				if err := _StakeAmountReader.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_StakeAmountReader *StakeAmountReaderFilterer) ParseOwnershipTransferred(log types.Log) (*StakeAmountReaderOwnershipTransferred, error) {
	event := new(StakeAmountReaderOwnershipTransferred)
	if err := _StakeAmountReader.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// StakeAmountReaderSetOperatorIterator is returned from FilterSetOperator and is used to iterate over the raw logs and unpacked data for SetOperator events raised by the StakeAmountReader contract.
type StakeAmountReaderSetOperatorIterator struct {
	Event *StakeAmountReaderSetOperator // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *StakeAmountReaderSetOperatorIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(StakeAmountReaderSetOperator)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(StakeAmountReaderSetOperator)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *StakeAmountReaderSetOperatorIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *StakeAmountReaderSetOperatorIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// StakeAmountReaderSetOperator represents a SetOperator event raised by the StakeAmountReader contract.
type StakeAmountReaderSetOperator struct {
	Operator common.Address
	IsAdd    bool
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterSetOperator is a free log retrieval operation binding the contract event 0x1618a22a3b00b9ac70fd5a82f1f5cdd8cb272bd0f1b740ddf7c26ab05881dd5b.
//
// Solidity: event SetOperator(address _operator, bool isAdd)
func (_StakeAmountReader *StakeAmountReaderFilterer) FilterSetOperator(opts *bind.FilterOpts) (*StakeAmountReaderSetOperatorIterator, error) {

	logs, sub, err := _StakeAmountReader.contract.FilterLogs(opts, "SetOperator")
	if err != nil {
		return nil, err
	}
	return &StakeAmountReaderSetOperatorIterator{contract: _StakeAmountReader.contract, event: "SetOperator", logs: logs, sub: sub}, nil
}

// WatchSetOperator is a free log subscription operation binding the contract event 0x1618a22a3b00b9ac70fd5a82f1f5cdd8cb272bd0f1b740ddf7c26ab05881dd5b.
//
// Solidity: event SetOperator(address _operator, bool isAdd)
func (_StakeAmountReader *StakeAmountReaderFilterer) WatchSetOperator(opts *bind.WatchOpts, sink chan<- *StakeAmountReaderSetOperator) (event.Subscription, error) {

	logs, sub, err := _StakeAmountReader.contract.WatchLogs(opts, "SetOperator")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(StakeAmountReaderSetOperator)
				if err := _StakeAmountReader.contract.UnpackLog(event, "SetOperator", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseSetOperator is a log parse operation binding the contract event 0x1618a22a3b00b9ac70fd5a82f1f5cdd8cb272bd0f1b740ddf7c26ab05881dd5b.
//
// Solidity: event SetOperator(address _operator, bool isAdd)
func (_StakeAmountReader *StakeAmountReaderFilterer) ParseSetOperator(log types.Log) (*StakeAmountReaderSetOperator, error) {
	event := new(StakeAmountReaderSetOperator)
	if err := _StakeAmountReader.contract.UnpackLog(event, "SetOperator", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

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
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"stakeAmount\",\"type\":\"uint256\"}],\"name\":\"_SetAmount\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"getSelfSetAmount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"addr\",\"type\":\"address\"}],\"name\":\"getSetAmount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"newStakeAmount\",\"type\":\"uint256\"}],\"name\":\"setAmount\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"stakeAmount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
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

// GetSelfSetAmount is a free data retrieval call binding the contract method 0xf3cffb9a.
//
// Solidity: function getSelfSetAmount() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCaller) GetSelfSetAmount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _StakeAmountReader.contract.Call(opts, &out, "getSelfSetAmount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetSelfSetAmount is a free data retrieval call binding the contract method 0xf3cffb9a.
//
// Solidity: function getSelfSetAmount() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderSession) GetSelfSetAmount() (*big.Int, error) {
	return _StakeAmountReader.Contract.GetSelfSetAmount(&_StakeAmountReader.CallOpts)
}

// GetSelfSetAmount is a free data retrieval call binding the contract method 0xf3cffb9a.
//
// Solidity: function getSelfSetAmount() view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCallerSession) GetSelfSetAmount() (*big.Int, error) {
	return _StakeAmountReader.Contract.GetSelfSetAmount(&_StakeAmountReader.CallOpts)
}

// GetSetAmount is a free data retrieval call binding the contract method 0xfd8daf2c.
//
// Solidity: function getSetAmount(address addr) view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCaller) GetSetAmount(opts *bind.CallOpts, addr common.Address) (*big.Int, error) {
	var out []interface{}
	err := _StakeAmountReader.contract.Call(opts, &out, "getSetAmount", addr)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetSetAmount is a free data retrieval call binding the contract method 0xfd8daf2c.
//
// Solidity: function getSetAmount(address addr) view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderSession) GetSetAmount(addr common.Address) (*big.Int, error) {
	return _StakeAmountReader.Contract.GetSetAmount(&_StakeAmountReader.CallOpts, addr)
}

// GetSetAmount is a free data retrieval call binding the contract method 0xfd8daf2c.
//
// Solidity: function getSetAmount(address addr) view returns(uint256)
func (_StakeAmountReader *StakeAmountReaderCallerSession) GetSetAmount(addr common.Address) (*big.Int, error) {
	return _StakeAmountReader.Contract.GetSetAmount(&_StakeAmountReader.CallOpts, addr)
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

// SetAmount is a paid mutator transaction binding the contract method 0x271f88b4.
//
// Solidity: function setAmount(uint256 newStakeAmount) returns()
func (_StakeAmountReader *StakeAmountReaderTransactor) SetAmount(opts *bind.TransactOpts, newStakeAmount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.contract.Transact(opts, "setAmount", newStakeAmount)
}

// SetAmount is a paid mutator transaction binding the contract method 0x271f88b4.
//
// Solidity: function setAmount(uint256 newStakeAmount) returns()
func (_StakeAmountReader *StakeAmountReaderSession) SetAmount(newStakeAmount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.SetAmount(&_StakeAmountReader.TransactOpts, newStakeAmount)
}

// SetAmount is a paid mutator transaction binding the contract method 0x271f88b4.
//
// Solidity: function setAmount(uint256 newStakeAmount) returns()
func (_StakeAmountReader *StakeAmountReaderTransactorSession) SetAmount(newStakeAmount *big.Int) (*types.Transaction, error) {
	return _StakeAmountReader.Contract.SetAmount(&_StakeAmountReader.TransactOpts, newStakeAmount)
}

// StakeAmountReaderSetAmountIterator is returned from FilterSetAmount and is used to iterate over the raw logs and unpacked data for SetAmount events raised by the StakeAmountReader contract.
type StakeAmountReaderSetAmountIterator struct {
	Event *StakeAmountReaderSetAmount // Event containing the contract specifics and raw log

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
func (it *StakeAmountReaderSetAmountIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(StakeAmountReaderSetAmount)
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
		it.Event = new(StakeAmountReaderSetAmount)
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
func (it *StakeAmountReaderSetAmountIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *StakeAmountReaderSetAmountIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// StakeAmountReaderSetAmount represents a SetAmount event raised by the StakeAmountReader contract.
type StakeAmountReaderSetAmount struct {
	Account     common.Address
	StakeAmount *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterSetAmount is a free log retrieval operation binding the contract event 0x7a6ada1be920976e7e3e0bc0e17cc7a4b8fb84f4a3e46d539f3b021c505e9646.
//
// Solidity: event _SetAmount(address indexed account, uint256 stakeAmount)
func (_StakeAmountReader *StakeAmountReaderFilterer) FilterSetAmount(opts *bind.FilterOpts, account []common.Address) (*StakeAmountReaderSetAmountIterator, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _StakeAmountReader.contract.FilterLogs(opts, "_SetAmount", accountRule)
	if err != nil {
		return nil, err
	}
	return &StakeAmountReaderSetAmountIterator{contract: _StakeAmountReader.contract, event: "_SetAmount", logs: logs, sub: sub}, nil
}

// WatchSetAmount is a free log subscription operation binding the contract event 0x7a6ada1be920976e7e3e0bc0e17cc7a4b8fb84f4a3e46d539f3b021c505e9646.
//
// Solidity: event _SetAmount(address indexed account, uint256 stakeAmount)
func (_StakeAmountReader *StakeAmountReaderFilterer) WatchSetAmount(opts *bind.WatchOpts, sink chan<- *StakeAmountReaderSetAmount, account []common.Address) (event.Subscription, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _StakeAmountReader.contract.WatchLogs(opts, "_SetAmount", accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(StakeAmountReaderSetAmount)
				if err := _StakeAmountReader.contract.UnpackLog(event, "_SetAmount", log); err != nil {
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

// ParseSetAmount is a log parse operation binding the contract event 0x7a6ada1be920976e7e3e0bc0e17cc7a4b8fb84f4a3e46d539f3b021c505e9646.
//
// Solidity: event _SetAmount(address indexed account, uint256 stakeAmount)
func (_StakeAmountReader *StakeAmountReaderFilterer) ParseSetAmount(log types.Log) (*StakeAmountReaderSetAmount, error) {
	event := new(StakeAmountReaderSetAmount)
	if err := _StakeAmountReader.contract.UnpackLog(event, "_SetAmount", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

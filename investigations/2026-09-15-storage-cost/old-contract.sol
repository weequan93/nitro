// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

import "./interfaces/IPriceFeed.sol";

contract PriceFeed is IPriceFeed {
    event SetLatestAnswer(address _token, int256 _answer, uint80 id);

    string public override description = "PriceFeed";
    uint8 public decimals = 8;
    address public gov;

    mapping(address => int256) public answer;
    mapping(address => uint80) public roundId;
    mapping(address => mapping (uint80 => int256)) public answers;
    mapping (address => bool) public isAdmin;

    struct TokenInfo{
        address token;
        int256 answerFeed;
    }

    constructor() {
        gov = msg.sender;
        isAdmin[msg.sender] = true;
    }

    modifier onlyGov() {
        require(msg.sender == gov, "gov err");
        _;
    }

    function setGov(address _gov) external onlyGov {
        require(_gov != address(0), "_gov err");
        gov = _gov;
    }

    function setAdmin(address _account, bool _isAdmin) external {
        require(msg.sender == gov, "PriceFeed: forbidden");
        isAdmin[_account] = _isAdmin;
    }

    function latestAnswer(address _token) external override view returns (int256) {
        return answer[_token];
    }

    function latestRound(address _token) external override view returns (uint80) {
        return roundId[_token];
    }

    function batchSetLatestAnswer(TokenInfo[] memory bInfo) external {
        require(isAdmin[msg.sender], "PriceFeed: forbidden");
        uint256 len = bInfo.length;
        require(len > 0, "len err");

        for(uint256 i = 0; i < len; i++) {
            _setLatestAnswer(bInfo[i].token, bInfo[i].answerFeed);
        }        
    }

    function _setLatestAnswer(address _token, int256 _answer) internal {
        uint80 id = roundId[_token] + 1;
        roundId[_token] = id;
        answer[_token] = _answer;
        answers[_token][id] = _answer;

        emit SetLatestAnswer(_token, _answer, id);
    }

    function setLatestAnswer(address _token, int256 _answer) external {
        require(isAdmin[msg.sender], "PriceFeed: forbidden");

        _setLatestAnswer(_token, _answer);
    }

    // returns roundId, answer, startedAt, updatedAt, answeredInRound
    function getRoundData(address _token, uint80 _roundId) external override view
        returns (uint80, int256, uint256, uint256, uint80)
    {
        return (_roundId, answers[_token][_roundId], 0, 0, 0);
    }
}

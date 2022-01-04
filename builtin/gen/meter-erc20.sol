// Copyright (c) 2018 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

pragma solidity 0.4.24;
import "./token.sol";

interface IMeterNative {
    function native_mtr_totalSupply() external view returns(uint256);
    function native_mtr_totalBurned() external view  returns(uint256);
    function native_mtr_get(address addr) external view returns(uint256);
    function native_mtr_add(address addr, uint256 amount) external;
    function native_mtr_sub(address addr, uint256 amount) external returns(bool);
    function native_mtr_locked_get(address addr) external view returns(uint256);
    function native_mtr_locked_add(address addr, uint256 amount) external;
    function native_mtr_locked_sub(address addr, uint256 amount) external returns(bool);

    //@@@@@
    function native_mtrg_totalSupply() external view returns(uint256);
    function native_mtrg_totalBurned() external view returns(uint256);
    function native_mtrg_get(address addr) external view returns(uint256);
    function native_mtrg_add(address addr, uint256 amount) external;
    function native_mtrg_sub(address addr, uint256 amount) external returns(bool);
    function native_mtrg_locked_get(address addr) external view returns(uint256);
    function native_mtrg_locked_add(address addr, uint256 amount) external;
    function native_mtrg_locked_sub(address addr, uint256 amount) external returns(bool);

    //@@@
    function native_master(address addr) external view returns(address);
}

contract NewMeterNative is IMeterNative {
    event MeterTrackerEvent(address _address, uint256 _amount, string _method);

    constructor () public payable {

    }

    function native_mtr_totalSupply() public view returns(uint256) {
        emit MeterTrackerEvent(msg.sender, uint256(0), "native_mtr_totalSupply");
        return uint256(0);
    }

    function native_mtr_totalBurned() public view returns(uint256) {
        emit MeterTrackerEvent(msg.sender, uint256(0), "native_mtr_totalBurned");
        return uint256(0);
    }

    function native_mtr_get(address addr) public view returns(uint256) {
        emit MeterTrackerEvent(addr, uint256(0), "native_mtr_get");
        return uint256(0);
    }

    function native_mtr_add(address addr, uint256 amount) public {
        emit MeterTrackerEvent(addr, amount, "native_mtr_add");
        return;
    }

    function native_mtr_sub(address addr, uint256 amount) public returns(bool) {
        emit MeterTrackerEvent(addr, amount, "native_mtr_sub");
        return true;
    }

    function native_mtr_locked_get(address addr) public view returns(uint256) {
        emit MeterTrackerEvent(addr, uint256(0), "native_mtr_locked_get");
        return uint256(0);
    }

    function native_mtr_locked_add(address addr, uint256 amount) public {
        emit MeterTrackerEvent(addr, amount, "native_mtr_locked_add");
        return;
    }

    function native_mtr_locked_sub(address addr, uint256 amount) public returns(bool) {
        emit MeterTrackerEvent(addr, amount, "native_mtr_locked_sub");
        return true;
    }

    //@@@@@
    function native_mtrg_totalSupply() public view returns(uint256) {
        emit MeterTrackerEvent(msg.sender, uint256(0), "native_mtrg_totalSupply");
        return uint256(0x0);
    }

    function native_mtrg_totalBurned() public view returns(uint256) {
        emit MeterTrackerEvent(msg.sender, uint256(0), "native_mtrg_totalBurned");
        return uint256(0);
    }

    function native_mtrg_get(address addr) public view returns(uint256) {
        emit MeterTrackerEvent(addr, uint256(0), "native_mtrg_get");
        return uint256(0);
    }

    function native_mtrg_add(address addr, uint256 amount) public {
        emit MeterTrackerEvent(addr, amount, "native_mtrg_add");
        return;
    }

    function native_mtrg_sub(address addr, uint256 amount) public returns(bool) {
        emit MeterTrackerEvent(addr, amount, "native_mtrg_sub");
        return true;
    }

    function native_mtrg_locked_get(address addr) public view returns(uint256) {
        emit MeterTrackerEvent(addr, uint256(0), "native_mtrg_locked_get");
        return uint256(0);
    }

    function native_mtrg_locked_add(address addr, uint256 amount) public {
        emit MeterTrackerEvent(addr, amount, "native_mtrg_locked_add");
        return;
    }

    function native_mtrg_locked_sub(address addr, uint256 amount) public returns(bool) {
        emit MeterTrackerEvent(addr, amount, "native_mtrg_locked_sub");
        return true;
    }

    //@@@
    function native_master(address addr) public view returns(address) {
        emit MeterTrackerEvent(addr, uint256(0), "native_master");
        return address(0x0);
    }
}

/// @title Meter implements VIP180(ERC20) standard, to present Meter/ Meter Gov tokens.
contract MeterERC20 is _Token {
    mapping(address => mapping(address => uint256)) allowed;

    function name() public pure returns(string) {
        return "STP Token";
    }

    function decimals() public pure returns(uint8) {
        return 18;
    }

    function symbol() public pure returns(string) {
        return "STPT";
    }

    function totalSupply() public view returns(uint256) {
        return NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_mtr_totalSupply();
    }

    // @return energy that total burned.
    function totalBurned() public view returns(uint256) {
        return NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_mtr_totalBurned();
    }

    function balanceOf(address _owner) public view returns(uint256 balance) {
        return NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_mtr_get(address (_owner));

    }

    function transfer(address _to, uint256 _amount) public returns(bool success) {
        _transfer(msg.sender, _to, _amount);
        return true;
    }

    /// @notice It's not VIP180(ERC20)'s standard method. It allows master of `_from` or `_from` itself to transfer `_amount` of energy to `_to`.
    function move(address _from, address _to, uint256 _amount) public returns(bool success) {
        require(_from == msg.sender || NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_master(_from) == msg.sender, "builtin: self or master required");
        _transfer(_from, _to, _amount);
        return true;
    }

    function transferFrom(address _from, address _to, uint256 _amount) public returns(bool success) {
        require(allowed[_from][msg.sender] >= _amount, "builtin: insufficient allowance");
        allowed[_from][msg.sender] -= _amount;

        _transfer(_from, _to, _amount);
        return true;
    }

    function allowance(address _owner, address _spender)  public view returns(uint256 remaining) {
        return allowed[_owner][_spender];
    }

    function approve(address _spender, uint256 _value) public returns(bool success){
        allowed[msg.sender][_spender] = _value;
        emit Approval(msg.sender, _spender, _value);
        return true;
    }

    function _transfer(address _from, address _to, uint256 _amount) internal {
        if (_amount > 0) {
            require(NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_mtr_sub(_from, _amount), "builtin: insufficient balance");
            // believed that will never overflow
            NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_mtr_add(_to, _amount);
        }
        emit Transfer(_from, _to, _amount);
    }
}

contract MeterGovERC20 is _Token {
    mapping(address => mapping(address => uint256)) allowed;

    function name() public pure returns(string) {
        return "Verse Network";
    }

    function decimals() public pure returns(uint8) {
        return 18;
    }

    function symbol() public pure returns(string) {
        return "VERSE";
    }

    function totalSupply() public view returns(uint256) {
        return NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_mtrg_totalSupply();
    }

    // @return energy that total burned.
    function totalBurned() public view returns(uint256) {
        return NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_mtrg_totalBurned();
    }

    function balanceOf(address _owner) public view returns(uint256 balance) {
        return NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_mtrg_get(_owner);
    }

    function transfer(address _to, uint256 _amount) public returns(bool success) {
        _transfer(msg.sender, _to, _amount);
        return true;
    }

    /// @notice It's not VIP180(ERC20)'s standard method. It allows master of `_from` or `_from` itself to transfer `_amount` of energy to `_to`.
    function move(address _from, address _to, uint256 _amount) public returns(bool success) {
        require(_from == msg.sender || NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_master(_from) == msg.sender, "builtin: self or master required");
        _transfer(_from, _to, _amount);
        return true;
    }

    function transferFrom(address _from, address _to, uint256 _amount) public returns(bool success) {
        require(allowed[_from][msg.sender] >= _amount, "builtin: insufficient allowance");
        allowed[_from][msg.sender] -= _amount;

        _transfer(_from, _to, _amount);
        return true;
    }

    function allowance(address _owner, address _spender)  public view returns(uint256 remaining) {
        return allowed[_owner][_spender];
    }

    function approve(address _spender, uint256 _value) public returns(bool success){
        allowed[msg.sender][_spender] = _value;
        emit Approval(msg.sender, _spender, _value);
        return true;
    }

    function _transfer(address _from, address _to, uint256 _amount) internal {
        if (_amount > 0) {
            require(NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_mtrg_sub(_from, _amount), "builtin: insufficient balance");
            // believed that will never overflow
            NewMeterNative(0x0000000000004E65774D657465724E6174697665).native_mtrg_add(_to, _amount);
        }
        emit Transfer(_from, _to, _amount);
    }
}

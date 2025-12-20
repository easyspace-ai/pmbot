package client

// CTFABI Conditional Token Framework 合约 ABI
// 包含 splitPosition, mergePositions, redeemPositions 等函数定义
const CTFABI = `[
	{
		"inputs": [
			{"name": "collateralToken", "type": "address"},
			{"name": "parentCollectionId", "type": "bytes32"},
			{"name": "conditionId", "type": "bytes32"},
			{"name": "partition", "type": "uint256[]"},
			{"name": "amount", "type": "uint256"}
		],
		"name": "splitPosition",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "collateralToken", "type": "address"},
			{"name": "parentCollectionId", "type": "bytes32"},
			{"name": "conditionId", "type": "bytes32"},
			{"name": "partition", "type": "uint256[]"},
			{"name": "amount", "type": "uint256"}
		],
		"name": "mergePositions",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "collateralToken", "type": "address"},
			{"name": "parentCollectionId", "type": "bytes32"},
			{"name": "conditionId", "type": "bytes32"},
			{"name": "indexSets", "type": "uint256[]"}
		],
		"name": "redeemPositions",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "oracle", "type": "address"},
			{"name": "questionId", "type": "bytes32"},
			{"name": "outcomeSlotCount", "type": "uint256"}
		],
		"name": "getConditionId",
		"outputs": [
			{"name": "", "type": "bytes32"}
		],
		"stateMutability": "pure",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "parentCollectionId", "type": "bytes32"},
			{"name": "conditionId", "type": "bytes32"},
			{"name": "indexSet", "type": "uint256"}
		],
		"name": "getCollectionId",
		"outputs": [
			{"name": "", "type": "bytes32"}
		],
		"stateMutability": "pure",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "collateralToken", "type": "address"},
			{"name": "collectionId", "type": "bytes32"}
		],
		"name": "getPositionId",
		"outputs": [
			{"name": "", "type": "uint256"}
		],
		"stateMutability": "pure",
		"type": "function"
	}
]`


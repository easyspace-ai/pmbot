package client

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"polymarket-btc-bot/internal/clob/types"
)

// CTFClient CTF合约客户端
type CTFClient struct {
	client          *ethclient.Client
	ctfAddress      common.Address
	collateralToken common.Address
	privateKey      *ecdsa.PrivateKey
	chainID         *big.Int
	ctfABI          abi.ABI
}

// GetCollateralToken 获取抵押品代币地址
func (c *CTFClient) GetCollateralToken() common.Address {
	return c.collateralToken
}

// GetCTFAddress 获取CTF合约地址
func (c *CTFClient) GetCTFAddress() common.Address {
	return c.ctfAddress
}

// NewCTFClient 创建新的CTF客户端
func NewCTFClient(
	rpcURL string,
	chainID types.Chain,
	privateKey *ecdsa.PrivateKey,
) (*CTFClient, error) {
	// 连接到以太坊节点
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("连接RPC节点失败: %w", err)
	}

	// 获取合约配置
	config, err := GetContractConfig(chainID)
	if err != nil {
		return nil, fmt.Errorf("获取合约配置失败: %w", err)
	}

	// 解析ABI
	ctfABI, err := abi.JSON(strings.NewReader(CTFABI))
	if err != nil {
		return nil, fmt.Errorf("解析CTF ABI失败: %w", err)
	}

	return &CTFClient{
		client:          client,
		ctfAddress:      common.HexToAddress(config.ConditionalTokens),
		collateralToken: common.HexToAddress(config.Collateral),
		privateKey:      privateKey,
		chainID:         big.NewInt(int64(chainID)),
		ctfABI:          ctfABI,
	}, nil
}

// GetConditionId 计算conditionId
// conditionId = keccak256(abi.encodePacked(oracle, questionId, outcomeSlotCount))
func (c *CTFClient) GetConditionId(oracle common.Address, questionId common.Hash, outcomeSlotCount *big.Int) (common.Hash, error) {
	// 使用合约的pure函数计算
	data, err := c.ctfABI.Pack("getConditionId", oracle, questionId, outcomeSlotCount)
	if err != nil {
		return common.Hash{}, fmt.Errorf("打包getConditionId参数失败: %w", err)
	}

	// 调用合约的view函数
	result, err := c.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &c.ctfAddress,
		Data: data,
	}, nil)
	if err != nil {
		return common.Hash{}, fmt.Errorf("调用getConditionId失败: %w", err)
	}

	var conditionId common.Hash
	if err := c.ctfABI.UnpackIntoInterface(&conditionId, "getConditionId", result); err != nil {
		return common.Hash{}, fmt.Errorf("解析getConditionId结果失败: %w", err)
	}

	return conditionId, nil
}

// GetCollectionId 计算collectionId
// collectionId = keccak256(abi.encodePacked(parentCollectionId, conditionId, indexSet))
func (c *CTFClient) GetCollectionId(parentCollectionId common.Hash, conditionId common.Hash, indexSet *big.Int) (common.Hash, error) {
	data, err := c.ctfABI.Pack("getCollectionId", parentCollectionId, conditionId, indexSet)
	if err != nil {
		return common.Hash{}, fmt.Errorf("打包getCollectionId参数失败: %w", err)
	}

	result, err := c.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &c.ctfAddress,
		Data: data,
	}, nil)
	if err != nil {
		return common.Hash{}, fmt.Errorf("调用getCollectionId失败: %w", err)
	}

	var collectionId common.Hash
	if err := c.ctfABI.UnpackIntoInterface(&collectionId, "getCollectionId", result); err != nil {
		return common.Hash{}, fmt.Errorf("解析getCollectionId结果失败: %w", err)
	}

	return collectionId, nil
}

// GetPositionId 计算positionId
// positionId = uint256(keccak256(abi.encodePacked(collateralToken, collectionId)))
func (c *CTFClient) GetPositionId(collateralToken common.Address, collectionId common.Hash) (*big.Int, error) {
	data, err := c.ctfABI.Pack("getPositionId", collateralToken, collectionId)
	if err != nil {
		return nil, fmt.Errorf("打包getPositionId参数失败: %w", err)
	}

	result, err := c.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &c.ctfAddress,
		Data: data,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("调用getPositionId失败: %w", err)
	}

	var positionId *big.Int
	if err := c.ctfABI.UnpackIntoInterface(&positionId, "getPositionId", result); err != nil {
		return nil, fmt.Errorf("解析getPositionId结果失败: %w", err)
	}

	return positionId, nil
}

// SplitPositionParams 拆分仓位参数
type SplitPositionParams struct {
	ConditionId string  // conditionId (hex string)
	Amount      float64 // 要拆分的USDC数量
}

// SplitPosition 拆分USDC为完整的仓位集合（1 YES + 1 NO）
// 1 USDC -> 1 YES + 1 NO
// 返回签名后的交易，需要调用SendTransaction发送
func (c *CTFClient) SplitPosition(ctx context.Context, params SplitPositionParams) (*ethtypes.Transaction, error) {
	// 验证参数
	if params.ConditionId == "" {
		return nil, fmt.Errorf("conditionId不能为空")
	}
	if params.Amount <= 0 {
		return nil, fmt.Errorf("拆分数量必须大于0")
	}

	// 验证前置条件（余额和授权）
	if err := c.ValidateSplitPosition(ctx, params.Amount); err != nil {
		return nil, err
	}

	// 转换conditionId
	conditionId := common.HexToHash(params.ConditionId)
	if conditionId == (common.Hash{}) {
		return nil, fmt.Errorf("无效的conditionId: %s", params.ConditionId)
	}

	// 对于Polymarket二进制市场，parentCollectionId总是bytes32(0)
	parentCollectionId := common.Hash{}

	// partition = [1, 2] 表示完整的仓位集合（YES和NO）
	partition := []*big.Int{
		big.NewInt(1), // indexSet for YES (0b01)
		big.NewInt(2), // indexSet for NO (0b10)
	}

	// 转换金额为6位小数精度
	// 例如：1 USDC = 1000000 (6 decimals)
	amount := new(big.Int)
	amountFloat := new(big.Float).SetFloat64(params.Amount)
	decimals := new(big.Float).SetInt64(1000000) // 10^6
	amountFloat.Mul(amountFloat, decimals)
	amount, _ = amountFloat.Int(nil)

	// 打包函数调用数据
	data, err := c.ctfABI.Pack("splitPosition",
		c.collateralToken,
		parentCollectionId,
		conditionId,
		partition,
		amount,
	)
	if err != nil {
		return nil, fmt.Errorf("打包splitPosition参数失败: %w", err)
	}

	// 获取账户地址
	fromAddress := crypto.PubkeyToAddress(c.privateKey.PublicKey)

	// 获取nonce
	nonce, err := c.client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return nil, fmt.Errorf("获取nonce失败: %w", err)
	}

	// 获取gas价格
	gasPrice, err := c.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取gas价格失败: %w", err)
	}

	// 估算gas
	gasLimit, err := c.client.EstimateGas(ctx, ethereum.CallMsg{
		From:  fromAddress,
		To:    &c.ctfAddress,
		Data:  data,
		Value: big.NewInt(0),
	})
	if err != nil {
		return nil, fmt.Errorf("估算gas失败: %w", err)
	}

	// 创建交易
	tx := ethtypes.NewTransaction(
		nonce,
		c.ctfAddress,
		big.NewInt(0),
		gasLimit,
		gasPrice,
		data,
	)

	// 签名交易
	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewEIP155Signer(c.chainID), c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("签名交易失败: %w", err)
	}

	return signedTx, nil
}

// MergePositionsParams 合并仓位参数
type MergePositionsParams struct {
	ConditionId string  // conditionId (hex string)
	Amount      float64 // 要合并的完整集合数量（1个完整集合 = 1 YES + 1 NO）
}

// MergePositions 合并完整的仓位集合为USDC（1 YES + 1 NO -> 1 USDC）
// 返回签名后的交易，需要调用SendTransaction发送
func (c *CTFClient) MergePositions(ctx context.Context, params MergePositionsParams) (*ethtypes.Transaction, error) {
	// 验证参数
	if params.ConditionId == "" {
		return nil, fmt.Errorf("conditionId不能为空")
	}
	if params.Amount <= 0 {
		return nil, fmt.Errorf("合并数量必须大于0")
	}

	// 转换conditionId
	conditionId := common.HexToHash(params.ConditionId)
	if conditionId == (common.Hash{}) {
		return nil, fmt.Errorf("无效的conditionId: %s", params.ConditionId)
	}

	// 验证前置条件（YES和NO余额）
	if err := c.ValidateMergePositions(ctx, conditionId, params.Amount); err != nil {
		return nil, err
	}

	// 对于Polymarket二进制市场，parentCollectionId总是bytes32(0)
	parentCollectionId := common.Hash{}

	// partition = [1, 2] 表示完整的仓位集合（YES和NO）
	partition := []*big.Int{
		big.NewInt(1), // indexSet for YES (0b01)
		big.NewInt(2), // indexSet for NO (0b10)
	}

	// 转换金额为6位小数精度
	amount := new(big.Int)
	amountFloat := new(big.Float).SetFloat64(params.Amount)
	decimals := new(big.Float).SetInt64(1000000) // 10^6
	amountFloat.Mul(amountFloat, decimals)
	amount, _ = amountFloat.Int(nil)

	// 打包函数调用数据
	data, err := c.ctfABI.Pack("mergePositions",
		c.collateralToken,
		parentCollectionId,
		conditionId,
		partition,
		amount,
	)
	if err != nil {
		return nil, fmt.Errorf("打包mergePositions参数失败: %w", err)
	}

	// 获取账户地址
	fromAddress := crypto.PubkeyToAddress(c.privateKey.PublicKey)

	// 获取nonce
	nonce, err := c.client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return nil, fmt.Errorf("获取nonce失败: %w", err)
	}

	// 获取gas价格
	gasPrice, err := c.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取gas价格失败: %w", err)
	}

	// 估算gas
	gasLimit, err := c.client.EstimateGas(ctx, ethereum.CallMsg{
		From:  fromAddress,
		To:    &c.ctfAddress,
		Data:  data,
		Value: big.NewInt(0),
	})
	if err != nil {
		return nil, fmt.Errorf("估算gas失败: %w", err)
	}

	// 创建交易
	tx := ethtypes.NewTransaction(
		nonce,
		c.ctfAddress,
		big.NewInt(0),
		gasLimit,
		gasPrice,
		data,
	)

	// 签名交易
	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewEIP155Signer(c.chainID), c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("签名交易失败: %w", err)
	}

	return signedTx, nil
}

// SendTransaction 发送交易到区块链
func (c *CTFClient) SendTransaction(ctx context.Context, tx *ethtypes.Transaction) (common.Hash, error) {
	err := c.client.SendTransaction(ctx, tx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("发送交易失败: %w", err)
	}
	return tx.Hash(), nil
}

// WaitForTransaction 等待交易确认
func (c *CTFClient) WaitForTransaction(ctx context.Context, txHash common.Hash) (*ethtypes.Receipt, error) {
	receipt, err := c.client.TransactionReceipt(ctx, txHash)
	if err != nil {
		return nil, fmt.Errorf("获取交易回执失败: %w", err)
	}
	return receipt, nil
}

// ERC20ABI ERC20标准ABI（用于余额和授权检查）
const ERC20ABI = `[
	{
		"inputs": [{"name": "account", "type": "address"}],
		"name": "balanceOf",
		"outputs": [{"name": "", "type": "uint256"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [
			{"name": "owner", "type": "address"},
			{"name": "spender", "type": "address"}
		],
		"name": "allowance",
		"outputs": [{"name": "", "type": "uint256"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

// ERC1155ABI ERC1155标准ABI（用于条件代币余额检查）
const ERC1155ABI = `[
	{
		"inputs": [
			{"name": "account", "type": "address"},
			{"name": "id", "type": "uint256"}
		],
		"name": "balanceOf",
		"outputs": [{"name": "", "type": "uint256"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

// GetUSDCBalance 获取USDC余额（返回USDC数量，已转换为6位小数）
func (c *CTFClient) GetUSDCBalance(ctx context.Context) (float64, error) {
	erc20ABI, err := abi.JSON(strings.NewReader(ERC20ABI))
	if err != nil {
		return 0, fmt.Errorf("解析ERC20 ABI失败: %w", err)
	}

	fromAddress := crypto.PubkeyToAddress(c.privateKey.PublicKey)
	data, err := erc20ABI.Pack("balanceOf", fromAddress)
	if err != nil {
		return 0, fmt.Errorf("打包balanceOf参数失败: %w", err)
	}

	result, err := c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &c.collateralToken,
		Data: data,
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("调用balanceOf失败: %w", err)
	}

	var balance *big.Int
	if err := erc20ABI.UnpackIntoInterface(&balance, "balanceOf", result); err != nil {
		return 0, fmt.Errorf("解析balanceOf结果失败: %w", err)
	}

	// 转换为6位小数格式
	balanceFloat := new(big.Float).SetInt(balance)
	decimals := new(big.Float).SetInt64(1000000) // 10^6
	balanceFloat.Quo(balanceFloat, decimals)
	balance64, _ := balanceFloat.Float64()

	return balance64, nil
}

// CheckUSDCAllowance 检查USDC授权给CTF合约的数量（返回USDC数量，已转换为6位小数）
func (c *CTFClient) CheckUSDCAllowance(ctx context.Context) (float64, error) {
	erc20ABI, err := abi.JSON(strings.NewReader(ERC20ABI))
	if err != nil {
		return 0, fmt.Errorf("解析ERC20 ABI失败: %w", err)
	}

	fromAddress := crypto.PubkeyToAddress(c.privateKey.PublicKey)
	data, err := erc20ABI.Pack("allowance", fromAddress, c.ctfAddress)
	if err != nil {
		return 0, fmt.Errorf("打包allowance参数失败: %w", err)
	}

	result, err := c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &c.collateralToken,
		Data: data,
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("调用allowance失败: %w", err)
	}

	var allowance *big.Int
	if err := erc20ABI.UnpackIntoInterface(&allowance, "allowance", result); err != nil {
		return 0, fmt.Errorf("解析allowance结果失败: %w", err)
	}

	// 转换为6位小数格式
	allowanceFloat := new(big.Float).SetInt(allowance)
	decimals := new(big.Float).SetInt64(1000000) // 10^6
	allowanceFloat.Quo(allowanceFloat, decimals)
	allowance64, _ := allowanceFloat.Float64()

	return allowance64, nil
}

// GetConditionalTokenBalance 获取条件代币余额（通过positionId）
// positionId: 条件代币的positionId（uint256）
// 返回代币数量（已转换为6位小数）
func (c *CTFClient) GetConditionalTokenBalance(ctx context.Context, positionId *big.Int) (float64, error) {
	erc1155ABI, err := abi.JSON(strings.NewReader(ERC1155ABI))
	if err != nil {
		return 0, fmt.Errorf("解析ERC1155 ABI失败: %w", err)
	}

	fromAddress := crypto.PubkeyToAddress(c.privateKey.PublicKey)
	data, err := erc1155ABI.Pack("balanceOf", fromAddress, positionId)
	if err != nil {
		return 0, fmt.Errorf("打包balanceOf参数失败: %w", err)
	}

	result, err := c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &c.ctfAddress,
		Data: data,
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("调用balanceOf失败: %w", err)
	}

	var balance *big.Int
	if err := erc1155ABI.UnpackIntoInterface(&balance, "balanceOf", result); err != nil {
		return 0, fmt.Errorf("解析balanceOf结果失败: %w", err)
	}

	// 转换为6位小数格式
	balanceFloat := new(big.Float).SetInt(balance)
	decimals := new(big.Float).SetInt64(1000000) // 10^6
	balanceFloat.Quo(balanceFloat, decimals)
	balance64, _ := balanceFloat.Float64()

	return balance64, nil
}

// ValidateSplitPosition 验证拆分操作的前置条件
// 检查USDC余额和授权是否足够
func (c *CTFClient) ValidateSplitPosition(ctx context.Context, amount float64) error {
	// 检查USDC余额
	balance, err := c.GetUSDCBalance(ctx)
	if err != nil {
		return fmt.Errorf("检查USDC余额失败: %w", err)
	}

	if balance < amount {
		return fmt.Errorf("USDC余额不足: 需要 %.6f USDC，当前余额 %.6f USDC", amount, balance)
	}

	// 检查USDC授权
	allowance, err := c.CheckUSDCAllowance(ctx)
	if err != nil {
		return fmt.Errorf("检查USDC授权失败: %w", err)
	}

	if allowance < amount {
		return fmt.Errorf("USDC授权不足: 需要 %.6f USDC，当前授权 %.6f USDC。请先授权USDC给CTF合约", amount, allowance)
	}

	return nil
}

// ValidateMergePositions 验证合并操作的前置条件
// 检查YES和NO代币余额是否足够
func (c *CTFClient) ValidateMergePositions(ctx context.Context, conditionId common.Hash, amount float64) error {
	// 计算YES和NO的positionId
	parentCollectionId := common.Hash{}
	
	// YES: indexSet = 1
	yesCollectionId, err := c.GetCollectionId(parentCollectionId, conditionId, big.NewInt(1))
	if err != nil {
		return fmt.Errorf("计算YES collectionId失败: %w", err)
	}
	yesPositionId, err := c.GetPositionId(c.collateralToken, yesCollectionId)
	if err != nil {
		return fmt.Errorf("计算YES positionId失败: %w", err)
	}

	// NO: indexSet = 2
	noCollectionId, err := c.GetCollectionId(parentCollectionId, conditionId, big.NewInt(2))
	if err != nil {
		return fmt.Errorf("计算NO collectionId失败: %w", err)
	}
	noPositionId, err := c.GetPositionId(c.collateralToken, noCollectionId)
	if err != nil {
		return fmt.Errorf("计算NO positionId失败: %w", err)
	}

	// 检查YES余额
	yesBalance, err := c.GetConditionalTokenBalance(ctx, yesPositionId)
	if err != nil {
		return fmt.Errorf("检查YES余额失败: %w", err)
	}
	if yesBalance < amount {
		return fmt.Errorf("YES代币余额不足: 需要 %.6f，当前余额 %.6f", amount, yesBalance)
	}

	// 检查NO余额
	noBalance, err := c.GetConditionalTokenBalance(ctx, noPositionId)
	if err != nil {
		return fmt.Errorf("检查NO余额失败: %w", err)
	}
	if noBalance < amount {
		return fmt.Errorf("NO代币余额不足: 需要 %.6f，当前余额 %.6f", amount, noBalance)
	}

	return nil
}


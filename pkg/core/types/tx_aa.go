package types

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/rlp"
	"golang.org/x/crypto/sha3"
)

// EIP-7701: Native Account Abstraction transaction type.
// Splits transaction scope into validation, execution, and post-operation steps.

const (
	// AATxType is the EIP-2718 envelope type for AA transactions.
	AATxType byte = 0x05

	// AABaseCost is the intrinsic gas cost of an AA transaction.
	AABaseCost uint64 = 15000
)

// AA execution roles per EIP-7701.
const (
	RoleSenderDeployment    uint8 = 0xA0
	RoleSenderValidation    uint8 = 0xA1
	RolePaymasterValidation uint8 = 0xA2
	RoleSenderExecution     uint8 = 0xA3
	RolePaymasterPostOp     uint8 = 0xA4
)

// AAEntryPoint is the canonical caller address for AA transaction frames.
var AAEntryPoint = HexToAddress("0x0000000000000000000000000000000000007701")

// AATx represents an EIP-7701 Native Account Abstraction transaction.
type AATx struct {
	ChainID              *big.Int
	Nonce                uint64
	Sender               Address
	SenderValidationData []byte
	Deployer             *Address  // nil if no deployer
	DeployerData         []byte
	Paymaster            *Address  // nil if no paymaster
	PaymasterData        []byte
	SenderExecutionData  []byte
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int

	// Per-phase gas limits.
	SenderValidationGas   uint64
	PaymasterValidationGas uint64
	SenderExecutionGas    uint64
	PaymasterPostOpGas    uint64

	AccessList        AccessList
	AuthorizationList []Authorization
}

// TxData interface implementation for AATx.

func (tx *AATx) txType() byte      { return AATxType }
func (tx *AATx) chainID() *big.Int  { return tx.ChainID }
func (tx *AATx) accessList() AccessList { return tx.AccessList }
func (tx *AATx) data() []byte       { return tx.SenderExecutionData }
func (tx *AATx) gas() uint64        { return tx.totalGas() }
func (tx *AATx) gasPrice() *big.Int { return tx.MaxFeePerGas }
func (tx *AATx) gasTipCap() *big.Int { return tx.MaxPriorityFeePerGas }
func (tx *AATx) gasFeeCap() *big.Int { return tx.MaxFeePerGas }
func (tx *AATx) value() *big.Int    { return new(big.Int) }
func (tx *AATx) nonce() uint64      { return tx.Nonce }
func (tx *AATx) to() *Address       { return &tx.Sender }

func (tx *AATx) totalGas() uint64 {
	total := AABaseCost
	total += tx.SenderValidationGas
	total += tx.PaymasterValidationGas
	total += tx.SenderExecutionGas
	total += tx.PaymasterPostOpGas
	return total
}

func (tx *AATx) copy() TxData {
	cpy := &AATx{
		Nonce:                  tx.Nonce,
		Sender:                 tx.Sender,
		SenderValidationData:   copyBytes(tx.SenderValidationData),
		DeployerData:           copyBytes(tx.DeployerData),
		PaymasterData:          copyBytes(tx.PaymasterData),
		SenderExecutionData:    copyBytes(tx.SenderExecutionData),
		SenderValidationGas:    tx.SenderValidationGas,
		PaymasterValidationGas: tx.PaymasterValidationGas,
		SenderExecutionGas:     tx.SenderExecutionGas,
		PaymasterPostOpGas:     tx.PaymasterPostOpGas,
		AccessList:             copyAccessList(tx.AccessList),
	}
	if tx.ChainID != nil {
		cpy.ChainID = new(big.Int).Set(tx.ChainID)
	}
	if tx.MaxPriorityFeePerGas != nil {
		cpy.MaxPriorityFeePerGas = new(big.Int).Set(tx.MaxPriorityFeePerGas)
	}
	if tx.MaxFeePerGas != nil {
		cpy.MaxFeePerGas = new(big.Int).Set(tx.MaxFeePerGas)
	}
	cpy.Deployer = copyAddressPtr(tx.Deployer)
	cpy.Paymaster = copyAddressPtr(tx.Paymaster)
	if tx.AuthorizationList != nil {
		cpy.AuthorizationList = make([]Authorization, len(tx.AuthorizationList))
		copy(cpy.AuthorizationList, tx.AuthorizationList)
	}
	return cpy
}

// --- RLP encoding/decoding ---

// aaTxRLP is the RLP encoding layout for AATx per EIP-7701.
type aaTxRLP struct {
	ChainID              *big.Int
	Nonce                uint64
	Sender               Address
	SenderValidationData []byte
	Deployer             []byte // empty for nil, 20 bytes otherwise
	DeployerData         []byte
	Paymaster            []byte // empty for nil, 20 bytes otherwise
	PaymasterData        []byte
	SenderExecutionData  []byte
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int
	SenderValidationGas   uint64
	PaymasterValidationGas uint64
	SenderExecutionGas    uint64
	PaymasterPostOpGas    uint64
	AccessList           []accessTupleRLP
	AuthorizationList    []authorizationRLP
}

// EncodeAATx encodes an AATx as a typed transaction envelope: 0x05 || RLP([...]).
func EncodeAATx(tx *AATx) ([]byte, error) {
	enc := aaTxRLP{
		ChainID:                bigOrZero(tx.ChainID),
		Nonce:                  tx.Nonce,
		Sender:                 tx.Sender,
		SenderValidationData:   tx.SenderValidationData,
		Deployer:               addressPtrToBytes(tx.Deployer),
		DeployerData:           tx.DeployerData,
		Paymaster:              addressPtrToBytes(tx.Paymaster),
		PaymasterData:          tx.PaymasterData,
		SenderExecutionData:    tx.SenderExecutionData,
		MaxPriorityFeePerGas:   bigOrZero(tx.MaxPriorityFeePerGas),
		MaxFeePerGas:           bigOrZero(tx.MaxFeePerGas),
		SenderValidationGas:    tx.SenderValidationGas,
		PaymasterValidationGas: tx.PaymasterValidationGas,
		SenderExecutionGas:     tx.SenderExecutionGas,
		PaymasterPostOpGas:     tx.PaymasterPostOpGas,
		AccessList:             encodeAccessList(tx.AccessList),
		AuthorizationList:      encodeAuthList(tx.AuthorizationList),
	}
	if enc.SenderValidationData == nil {
		enc.SenderValidationData = []byte{}
	}
	if enc.DeployerData == nil {
		enc.DeployerData = []byte{}
	}
	if enc.PaymasterData == nil {
		enc.PaymasterData = []byte{}
	}
	if enc.SenderExecutionData == nil {
		enc.SenderExecutionData = []byte{}
	}

	payload, err := rlp.EncodeToBytes(enc)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 1+len(payload))
	result[0] = AATxType
	copy(result[1:], payload)
	return result, nil
}

// DecodeAATx decodes the RLP payload (without the type byte) into an AATx.
func DecodeAATx(data []byte) (*AATx, error) {
	var dec aaTxRLP
	if err := rlp.DecodeBytes(data, &dec); err != nil {
		return nil, fmt.Errorf("decode aa tx: %w", err)
	}
	tx := &AATx{
		ChainID:                dec.ChainID,
		Nonce:                  dec.Nonce,
		Sender:                 dec.Sender,
		SenderValidationData:   dec.SenderValidationData,
		Deployer:               bytesToAddressPtr(dec.Deployer),
		DeployerData:           dec.DeployerData,
		Paymaster:              bytesToAddressPtr(dec.Paymaster),
		PaymasterData:          dec.PaymasterData,
		SenderExecutionData:    dec.SenderExecutionData,
		MaxPriorityFeePerGas:   dec.MaxPriorityFeePerGas,
		MaxFeePerGas:           dec.MaxFeePerGas,
		SenderValidationGas:    dec.SenderValidationGas,
		PaymasterValidationGas: dec.PaymasterValidationGas,
		SenderExecutionGas:     dec.SenderExecutionGas,
		PaymasterPostOpGas:     dec.PaymasterPostOpGas,
		AccessList:             decodeAccessList(dec.AccessList),
		AuthorizationList:      decodeAuthList(dec.AuthorizationList),
	}
	return tx, nil
}

// --- Signature hash ---

// ComputeAASigHash computes the canonical signature hash for an AATx.
// Result: keccak256(0x05 || rlp(tx))
func ComputeAASigHash(tx *AATx) Hash {
	enc := aaTxRLP{
		ChainID:                bigOrZero(tx.ChainID),
		Nonce:                  tx.Nonce,
		Sender:                 tx.Sender,
		SenderValidationData:   tx.SenderValidationData,
		Deployer:               addressPtrToBytes(tx.Deployer),
		DeployerData:           tx.DeployerData,
		Paymaster:              addressPtrToBytes(tx.Paymaster),
		PaymasterData:          tx.PaymasterData,
		SenderExecutionData:    tx.SenderExecutionData,
		MaxPriorityFeePerGas:   bigOrZero(tx.MaxPriorityFeePerGas),
		MaxFeePerGas:           bigOrZero(tx.MaxFeePerGas),
		SenderValidationGas:    tx.SenderValidationGas,
		PaymasterValidationGas: tx.PaymasterValidationGas,
		SenderExecutionGas:     tx.SenderExecutionGas,
		PaymasterPostOpGas:     tx.PaymasterPostOpGas,
		AccessList:             encodeAccessList(tx.AccessList),
		AuthorizationList:      encodeAuthList(tx.AuthorizationList),
	}
	if enc.SenderValidationData == nil {
		enc.SenderValidationData = []byte{}
	}
	if enc.DeployerData == nil {
		enc.DeployerData = []byte{}
	}
	if enc.PaymasterData == nil {
		enc.PaymasterData = []byte{}
	}
	if enc.SenderExecutionData == nil {
		enc.SenderExecutionData = []byte{}
	}

	payload, err := rlp.EncodeToBytes(enc)
	if err != nil {
		return Hash{}
	}
	d := sha3.NewLegacyKeccak256()
	d.Write([]byte{AATxType})
	d.Write(payload)
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// --- Validation ---

// ValidateAATx performs static validity checks on an AATx per EIP-7701.
func ValidateAATx(tx *AATx) error {
	if tx.ChainID != nil && tx.ChainID.Sign() < 0 {
		return errors.New("aa tx: negative chain ID")
	}
	if tx.MaxFeePerGas == nil || tx.MaxFeePerGas.Sign() < 0 {
		return errors.New("aa tx: invalid max fee per gas")
	}
	if tx.MaxPriorityFeePerGas == nil || tx.MaxPriorityFeePerGas.Sign() < 0 {
		return errors.New("aa tx: invalid max priority fee per gas")
	}
	if tx.MaxPriorityFeePerGas.Cmp(tx.MaxFeePerGas) > 0 {
		return errors.New("aa tx: max priority fee exceeds max fee")
	}
	if tx.SenderValidationGas == 0 {
		return errors.New("aa tx: sender validation gas must be > 0")
	}
	if tx.SenderExecutionGas == 0 {
		return errors.New("aa tx: sender execution gas must be > 0")
	}
	// If paymaster is set, paymaster validation gas must be > 0.
	if tx.Paymaster != nil {
		if tx.PaymasterValidationGas == 0 {
			return errors.New("aa tx: paymaster validation gas must be > 0 when paymaster set")
		}
	}
	// If deployer is set, sender must have no existing code.
	// (This is a dynamic check; we just validate deployer data is present.)
	if tx.Deployer != nil && len(tx.DeployerData) == 0 {
		return errors.New("aa tx: deployer set but deployer data is empty")
	}
	// Sender cannot be zero.
	if tx.Sender == (Address{}) {
		return errors.New("aa tx: sender is zero address")
	}
	return nil
}

package polymarket

import (
	"crypto/ecdsa"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	orderbuilder "github.com/polymarket/go-order-utils/pkg/builder"
	ordermodel "github.com/polymarket/go-order-utils/pkg/model"
)

const (
	defaultDynamicChainID       int64   = 137
	defaultDynamicPrice         float64 = 0.5
	defaultDynamicOrderType             = "GTC"
	defaultDynamicSide                  = "BUY"
	defaultExpirationSeconds    int64   = 600
	baseTokenDecimalsMultiplier         = 1_000_000.0
	zeroAddressHex                      = "0x0000000000000000000000000000000000000000"
)

type DynamicOrderConfig struct {
	PrivateKeyHex     string
	Owner             string
	MakerAddress      string
	SignerAddress     string
	TakerAddress      string
	Price             float64
	Side              string
	OrderType         string
	DeferExec         bool
	FeeRateBps        int64
	Nonce             int64
	NonceProvider     NonceProvider
	SignatureType     int64
	ExpirationSeconds int64
	ChainID           int64
	NegRisk           bool
	Now               func() time.Time
	SaltGenerator     func() int64
}

type NonceProvider interface {
	NextNonce() (int64, error)
}

type DynamicOrderBuilder struct {
	cfg         DynamicOrderConfig
	privateKey  *ecdsa.PrivateKey
	builder     orderbuilder.ExchangeOrderBuilder
	contract    ordermodel.VerifyingContract
	signer      string
	maker       string
	taker       string
	sideLabel   string
	sideEnum    ordermodel.Side
	orderType   string
	baseNowFunc func() time.Time
}

func NewDynamicOrderBuilder(cfg DynamicOrderConfig) (*DynamicOrderBuilder, error) {
	owner := strings.TrimSpace(cfg.Owner)
	if owner == "" {
		return nil, fmt.Errorf("dynamic order owner is required")
	}

	privateKey, signerAddress, err := parseSigner(cfg.PrivateKeyHex)
	if err != nil {
		return nil, err
	}

	if cfg.SignerAddress != "" && !strings.EqualFold(strings.TrimSpace(cfg.SignerAddress), signerAddress) {
		return nil, fmt.Errorf("configured signer address does not match provided private key")
	}

	maker := signerAddress
	if strings.TrimSpace(cfg.MakerAddress) != "" {
		maker = strings.TrimSpace(cfg.MakerAddress)
	}

	taker := zeroAddressHex
	if strings.TrimSpace(cfg.TakerAddress) != "" {
		taker = strings.TrimSpace(cfg.TakerAddress)
	}

	price := cfg.Price
	if price == 0 {
		price = defaultDynamicPrice
	}
	if !isValidProbabilityPrice(price) {
		return nil, fmt.Errorf("dynamic order price must be within (0, 1), got %.8f", price)
	}
	if cfg.NonceProvider == nil && cfg.Nonce < 0 {
		return nil, fmt.Errorf("dynamic nonce must be >= 0")
	}

	sideLabel, sideEnum, err := normalizeDynamicSide(cfg.Side)
	if err != nil {
		return nil, err
	}

	orderType, err := normalizeOrderType(cfg.OrderType)
	if err != nil {
		return nil, err
	}

	if cfg.SignatureType < 0 || cfg.SignatureType > int64(ordermodel.POLY_GNOSIS_SAFE) {
		return nil, fmt.Errorf("dynamic signature type must be 0, 1, or 2")
	}

	chainID := cfg.ChainID
	if chainID == 0 {
		chainID = defaultDynamicChainID
	}

	expirationSeconds := cfg.ExpirationSeconds
	if expirationSeconds == 0 {
		expirationSeconds = defaultExpirationSeconds
	}
	if expirationSeconds < 0 {
		return nil, fmt.Errorf("dynamic expiration seconds must be >= 0")
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	builder := orderbuilder.NewExchangeOrderBuilderImpl(big.NewInt(chainID), cfg.SaltGenerator)
	contract := ordermodel.CTFExchange
	if cfg.NegRisk {
		contract = ordermodel.NegRiskCTFExchange
	}

	cfg.Owner = owner
	cfg.Price = price
	cfg.Side = sideLabel
	cfg.OrderType = orderType
	cfg.ExpirationSeconds = expirationSeconds
	cfg.ChainID = chainID

	return &DynamicOrderBuilder{
		cfg:         cfg,
		privateKey:  privateKey,
		builder:     builder,
		contract:    contract,
		signer:      signerAddress,
		maker:       maker,
		taker:       taker,
		sideLabel:   sideLabel,
		sideEnum:    sideEnum,
		orderType:   orderType,
		baseNowFunc: now,
	}, nil
}

func (b *DynamicOrderBuilder) BuildRequest(assetID string, notional float64) (PostOrderRequest, error) {
	return b.buildRequest(assetID, notional, 0, false)
}

func (b *DynamicOrderBuilder) BuildRequestWithPrice(assetID string, notional float64, price float64) (PostOrderRequest, error) {
	return b.buildRequest(assetID, notional, price, true)
}

func (b *DynamicOrderBuilder) buildRequest(assetID string, notional float64, overridePrice float64, usePriceOverride bool) (PostOrderRequest, error) {
	tokenID := strings.TrimSpace(assetID)
	if tokenID == "" {
		return PostOrderRequest{}, fmt.Errorf("dynamic order token id is required")
	}
	if notional <= 0 {
		return PostOrderRequest{}, fmt.Errorf("dynamic order notional must be > 0")
	}
	price, err := b.resolvePrice(overridePrice, usePriceOverride)
	if err != nil {
		return PostOrderRequest{}, err
	}
	nonce, err := b.resolveNonce()
	if err != nil {
		return PostOrderRequest{}, err
	}

	makerAmount, takerAmount, err := b.computeAmounts(notional, price)
	if err != nil {
		return PostOrderRequest{}, err
	}

	expiration := int64(0)
	if b.cfg.ExpirationSeconds > 0 {
		expiration = b.baseNowFunc().UTC().Unix() + b.cfg.ExpirationSeconds
	}

	orderData := &ordermodel.OrderData{
		Maker:         b.maker,
		Taker:         b.taker,
		TokenId:       tokenID,
		MakerAmount:   makerAmount,
		TakerAmount:   takerAmount,
		FeeRateBps:    strconv.FormatInt(b.cfg.FeeRateBps, 10),
		Nonce:         strconv.FormatInt(nonce, 10),
		Signer:        b.signer,
		Expiration:    strconv.FormatInt(expiration, 10),
		Side:          b.sideEnum,
		SignatureType: ordermodel.SignatureType(b.cfg.SignatureType),
	}

	signedOrder, err := b.builder.BuildSignedOrder(b.privateKey, orderData, b.contract)
	if err != nil {
		return PostOrderRequest{}, fmt.Errorf("build signed order: %w", err)
	}

	request := PostOrderRequest{
		Order: map[string]any{
			"salt":          signedOrder.Salt.String(),
			"maker":         signedOrder.Maker.Hex(),
			"signer":        signedOrder.Signer.Hex(),
			"taker":         signedOrder.Taker.Hex(),
			"tokenId":       signedOrder.TokenId.String(),
			"makerAmount":   signedOrder.MakerAmount.String(),
			"takerAmount":   signedOrder.TakerAmount.String(),
			"side":          b.sideLabel,
			"expiration":    signedOrder.Expiration.String(),
			"nonce":         signedOrder.Nonce.String(),
			"feeRateBps":    signedOrder.FeeRateBps.String(),
			"signatureType": signedOrder.SignatureType.Int64(),
			"signature":     hexutil.Encode(signedOrder.Signature),
		},
		Owner:     b.cfg.Owner,
		OrderType: b.orderType,
		DeferExec: b.cfg.DeferExec,
	}
	return request, nil
}

func (b *DynamicOrderBuilder) computeAmounts(notional float64, price float64) (string, string, error) {
	if !isFinitePositive(notional) {
		return "", "", fmt.Errorf("dynamic order notional must be finite and > 0")
	}
	size := notional / price
	if !isFinitePositive(size) {
		return "", "", fmt.Errorf("dynamic order computed size must be finite and > 0")
	}

	notionalUnits, err := toBaseUnits(notional)
	if err != nil {
		return "", "", err
	}
	sizeUnits, err := toBaseUnits(size)
	if err != nil {
		return "", "", err
	}

	if b.sideEnum == ordermodel.BUY {
		return notionalUnits, sizeUnits, nil
	}
	return sizeUnits, notionalUnits, nil
}

func (b *DynamicOrderBuilder) resolvePrice(overridePrice float64, usePriceOverride bool) (float64, error) {
	price := b.cfg.Price
	if usePriceOverride {
		price = overridePrice
	}
	if !isValidProbabilityPrice(price) {
		return 0, fmt.Errorf("dynamic order price must be within (0, 1), got %.8f", price)
	}
	return price, nil
}

func (b *DynamicOrderBuilder) resolveNonce() (int64, error) {
	if b.cfg.NonceProvider != nil {
		nonce, err := b.cfg.NonceProvider.NextNonce()
		if err != nil {
			return 0, fmt.Errorf("allocate dynamic nonce: %w", err)
		}
		if nonce < 0 {
			return 0, fmt.Errorf("allocated dynamic nonce must be >= 0")
		}
		return nonce, nil
	}
	if b.cfg.Nonce < 0 {
		return 0, fmt.Errorf("dynamic nonce must be >= 0")
	}
	return b.cfg.Nonce, nil
}

func parseSigner(privateKeyHex string) (*ecdsa.PrivateKey, string, error) {
	trimmed := strings.TrimSpace(privateKeyHex)
	trimmed = strings.TrimPrefix(trimmed, "0x")
	if trimmed == "" {
		return nil, "", fmt.Errorf("POLY_PRIVATE_KEY is required for dynamic order signing")
	}
	privateKey, err := crypto.HexToECDSA(trimmed)
	if err != nil {
		return nil, "", fmt.Errorf("parse POLY_PRIVATE_KEY: %w", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	return privateKey, address, nil
}

func normalizeDynamicSide(raw string) (string, ordermodel.Side, error) {
	side := strings.ToUpper(strings.TrimSpace(raw))
	if side == "" {
		side = defaultDynamicSide
	}
	switch side {
	case "BUY":
		return side, ordermodel.BUY, nil
	case "SELL":
		return side, ordermodel.SELL, nil
	default:
		return "", 0, fmt.Errorf("dynamic order side must be BUY or SELL")
	}
}

func normalizeOrderType(raw string) (string, error) {
	orderType := strings.ToUpper(strings.TrimSpace(raw))
	if orderType == "" {
		orderType = defaultDynamicOrderType
	}
	switch orderType {
	case "GTC", "FOK", "GTD", "FAK":
		return orderType, nil
	default:
		return "", fmt.Errorf("dynamic order type must be one of GTC/FOK/GTD/FAK")
	}
}

func toBaseUnits(value float64) (string, error) {
	if !isFinitePositive(value) {
		return "", fmt.Errorf("dynamic amount must be finite and > 0")
	}
	units := math.Round(value * baseTokenDecimalsMultiplier)
	if units <= 0 {
		return "", fmt.Errorf("dynamic amount produced non-positive base units")
	}
	return strconv.FormatInt(int64(units), 10), nil
}

func isFinitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

func isValidProbabilityPrice(price float64) bool {
	return isFinitePositive(price) && price < 1
}

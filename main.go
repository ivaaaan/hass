package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	mainnetInfoURL = "https://api.hyperliquid.xyz/info"
	testnetInfoURL = "https://api.hyperliquid-testnet.xyz/info"
)

type infoClient struct {
	url        string
	httpClient *http.Client
}

type infoRequest struct {
	Type string `json:"type"`
	Dex  string `json:"dex,omitempty"`
}

type metaResponse struct {
	Universe []perpAsset       `json:"universe"`
	Tables   []json.RawMessage `json:"marginTables"`
}

type perpAsset struct {
	Name string `json:"name"`
}

type perpDex struct {
	Name string `json:"name"`
}

type spotMetaResponse struct {
	Tokens   []spotToken `json:"tokens"`
	Universe []spotInfo  `json:"universe"`
}

type spotToken struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
}

type spotInfo struct {
	Name   string `json:"name"`
	Tokens []int  `json:"tokens"`
	Index  int    `json:"index"`
}

func main() {
	var testnet bool
	var spot bool

	flag.BoolVar(&testnet, "testnet", false, "use Hyperliquid testnet")
	flag.BoolVar(&spot, "spot", false, "look up a spot asset instead of a perp")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}

	url := mainnetInfoURL
	if testnet {
		url = testnetInfoURL
	}

	client := infoClient{
		url:        url,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}

	ctx := context.Background()
	symbol := flag.Arg(0)

	var (
		assetID int
		err     error
	)
	if spot {
		assetID, err = lookupSpotAssetID(ctx, client, symbol)
	} else {
		assetID, err = lookupPerpAssetID(ctx, client, symbol)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Println(assetID)
}

func usage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(flag.CommandLine.Output(), `Usage:
  %[1]s [--testnet] [--spot] SYMBOL

Examples:
  %[1]s BTC
  %[1]s xyz:XYZ100
  %[1]s --testnet BTC
  %[1]s --spot HYPE
  %[1]s --spot --testnet HYPE

`, name)
	flag.PrintDefaults()
}

func lookupPerpAssetID(ctx context.Context, client infoClient, symbol string) (int, error) {
	dex, coin, fullName, err := parsePerpSymbol(symbol)
	if err != nil {
		return 0, err
	}

	var meta metaResponse
	if err := client.postInfo(ctx, infoRequest{Type: "meta", Dex: dex}, &meta); err != nil {
		return 0, err
	}

	index := -1
	for i, asset := range meta.Universe {
		if sameName(asset.Name, coin) || sameName(asset.Name, fullName) {
			index = i
			break
		}
	}
	if index == -1 {
		return 0, fmt.Errorf("perp %q not found", symbol)
	}

	if dex == "" {
		return index, nil
	}

	dexIndex, err := lookupPerpDexIndex(ctx, client, dex)
	if err != nil {
		return 0, err
	}
	return 100000 + dexIndex*10000 + index, nil
}

func parsePerpSymbol(symbol string) (dex, coin, fullName string, err error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return "", "", "", errors.New("symbol is required")
	}

	if before, after, ok := strings.Cut(symbol, ":"); ok {
		dex = strings.ToLower(strings.TrimSpace(before))
		coin = strings.TrimSpace(after)
		if dex == "" || coin == "" {
			return "", "", "", fmt.Errorf("invalid perp symbol %q; expected dex:coin", symbol)
		}
		fullName = dex + ":" + coin
		return dex, coin, fullName, nil
	}

	coin = symbol
	return "", coin, coin, nil
}

func lookupPerpDexIndex(ctx context.Context, client infoClient, dex string) (int, error) {
	var dexes []*perpDex
	if err := client.postInfo(ctx, infoRequest{Type: "perpDexs"}, &dexes); err != nil {
		return 0, err
	}

	for i, d := range dexes {
		if d != nil && sameName(d.Name, dex) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("perp dex %q not found", dex)
}

func lookupSpotAssetID(ctx context.Context, client infoClient, symbol string) (int, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return 0, errors.New("symbol is required")
	}
	if strings.Contains(symbol, ":") {
		return 0, errors.New("spot symbols do not use a dex prefix")
	}

	var meta spotMetaResponse
	if err := client.postInfo(ctx, infoRequest{Type: "spotMeta"}, &meta); err != nil {
		return 0, err
	}

	for _, market := range meta.Universe {
		if sameName(market.Name, symbol) {
			return 10000 + market.Index, nil
		}
	}

	base, quote, err := parseSpotPair(symbol)
	if err != nil {
		return 0, err
	}
	tokensByIndex := make(map[int]spotToken, len(meta.Tokens))
	for _, token := range meta.Tokens {
		tokensByIndex[token.Index] = token
	}

	for _, market := range meta.Universe {
		if len(market.Tokens) < 2 {
			continue
		}
		baseToken, ok := tokensByIndex[market.Tokens[0]]
		if !ok {
			continue
		}
		quoteToken, ok := tokensByIndex[market.Tokens[1]]
		if !ok {
			continue
		}
		if sameName(baseToken.Name, base) && sameName(quoteToken.Name, quote) {
			return 10000 + market.Index, nil
		}
	}

	if strings.Contains(symbol, "/") {
		return 0, fmt.Errorf("spot market %q not found", symbol)
	}
	return 0, fmt.Errorf("spot market %q not found; tried %s/%s", symbol, base, quote)
}

func parseSpotPair(symbol string) (base, quote string, err error) {
	if before, after, ok := strings.Cut(symbol, "/"); ok {
		base = strings.TrimSpace(before)
		quote = strings.TrimSpace(after)
		if base == "" || quote == "" || strings.Contains(after, "/") {
			return "", "", fmt.Errorf("invalid spot symbol %q; expected BASE or BASE/QUOTE", symbol)
		}
		return base, quote, nil
	}
	return strings.TrimSpace(symbol), "USDC", nil
}

func sameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func (c infoClient) postInfo(ctx context.Context, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(string(data))
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("Hyperliquid info request failed: %s: %s", resp.Status, message)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode Hyperliquid response: %w", err)
	}
	return nil
}

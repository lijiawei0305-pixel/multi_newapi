// Package epay implements the small Epay submit/callback protocol surface used
// by the application. It intentionally has no dependency on an external SDK.
package epay

import (
	"crypto/md5" // #nosec G501 -- Epay's wire protocol requires MD5 signatures.
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"sort"
	"strings"
)

type Config struct {
	PartnerID string
	Key       string
}

type DeviceType string

const (
	PC     DeviceType = "pc"
	Mobile DeviceType = "mobile"

	StatusTradeSuccess = "TRADE_SUCCESS"
)

type PurchaseArgs struct {
	Type           string
	ServiceTradeNo string
	Name           string
	Money          string
	Device         DeviceType
	NotifyURL      *url.URL
	ReturnURL      *url.URL
}

type VerifyResult struct {
	Type           string
	TradeNo        string
	ServiceTradeNo string
	Name           string
	Money          string
	TradeStatus    string
	VerifyStatus   bool
}

type Client struct {
	config  Config
	baseURL *url.URL
}

func NewClient(config Config, baseURL string) (*Client, error) {
	if strings.TrimSpace(config.PartnerID) == "" || config.Key == "" {
		return nil, errors.New("Epay merchant configuration is incomplete")
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, errors.New("Epay base URL must be an absolute HTTP(S) URL without credentials")
	}
	return &Client{config: config, baseURL: u}, nil
}

func (c *Client) Purchase(args *PurchaseArgs) (string, map[string]string, error) {
	if c == nil || c.baseURL == nil || args == nil || args.NotifyURL == nil || args.ReturnURL == nil {
		return "", nil, errors.New("Epay purchase arguments are incomplete")
	}
	if args.Type == "" || args.ServiceTradeNo == "" || args.Name == "" || args.Money == "" {
		return "", nil, errors.New("Epay purchase fields are incomplete")
	}

	endpoint := *c.baseURL
	endpoint.Path = path.Join(endpoint.Path, "submit.php")
	params := map[string]string{
		"pid":          c.config.PartnerID,
		"type":         args.Type,
		"out_trade_no": args.ServiceTradeNo,
		"notify_url":   args.NotifyURL.String(),
		"return_url":   args.ReturnURL.String(),
		"name":         args.Name,
		"money":        args.Money,
		"device":       string(args.Device),
	}
	return endpoint.String(), signedParams(params, c.config.Key), nil
}

func (c *Client) Verify(params map[string]string) (*VerifyResult, error) {
	if c == nil || len(params) == 0 {
		return nil, errors.New("Epay callback parameters are empty")
	}
	if params["pid"] != c.config.PartnerID {
		return nil, errors.New("Epay callback merchant does not match")
	}
	if signType := strings.TrimSpace(params["sign_type"]); signType != "" && !strings.EqualFold(signType, "MD5") {
		return nil, errors.New("Epay callback signature type is unsupported")
	}
	receivedSignature := strings.ToLower(strings.TrimSpace(params["sign"]))
	if len(receivedSignature) != md5.Size*2 {
		return nil, errors.New("Epay callback signature is invalid")
	}
	expectedSignature := signature(params, c.config.Key)
	verified := subtle.ConstantTimeCompare([]byte(receivedSignature), []byte(expectedSignature)) == 1

	return &VerifyResult{
		Type:           params["type"],
		TradeNo:        params["trade_no"],
		ServiceTradeNo: params["out_trade_no"],
		Name:           params["name"],
		Money:          params["money"],
		TradeStatus:    params["trade_status"],
		VerifyStatus:   verified,
	}, nil
}

func signedParams(params map[string]string, key string) map[string]string {
	result := make(map[string]string, len(params)+2)
	for name, value := range params {
		result[name] = value
	}
	result["sign"] = signature(result, key)
	result["sign_type"] = "MD5"
	return result
}

func signature(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for name, value := range params {
		if value == "" || name == "sign" || name == "sign_type" {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)

	var payload strings.Builder
	for index, name := range keys {
		if index > 0 {
			payload.WriteByte('&')
		}
		payload.WriteString(name)
		payload.WriteByte('=')
		payload.WriteString(params[name])
	}
	payload.WriteString(key)
	digest := md5.Sum([]byte(payload.String())) // #nosec G401 -- required by the Epay protocol.
	return hex.EncodeToString(digest[:])
}

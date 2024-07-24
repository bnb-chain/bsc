package blockarchiver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	bundlesdk "github.com/bnb-chain/greenfield-bundle-sdk/bundle"
)

// Client is a client to interact with the block archiver service
type Client struct {
	hc                *http.Client
	blockArchiverHost string
	spHost            string
	bucketName        string
}

func New(blockAchieverHost, spHost, bucketName string) (*Client, error) {
	transport := &http.Transport{
		DisableCompression:  true,
		MaxIdleConnsPerHost: 1000,
		MaxConnsPerHost:     1000,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{
		Timeout:   10 * time.Minute,
		Transport: transport,
	}
	return &Client{hc: client, blockArchiverHost: blockAchieverHost, spHost: spHost, bucketName: bucketName}, nil
}

func (c *Client) GetBlockByHash(ctx context.Context, hash common.Hash) (*Block, error) {
	payload := preparePayload("eth_getBlockByHash", []interface{}{hash.String(), "true"})
	body, err := c.postRequest(ctx, payload)
	if err != nil {
		return nil, err
	}
	getBlockResp := GetBlockResponse{}
	err = json.Unmarshal(body, &getBlockResp)
	if err != nil {
		return nil, err
	}
	return getBlockResp.Result, nil
}

func (c *Client) GetBlockByNumber(ctx context.Context, number uint64) (*Block, error) {
	payload := preparePayload("eth_getBlockByNumber", []interface{}{Int64ToHex(int64(number)), "true"})
	body, err := c.postRequest(ctx, payload)
	if err != nil {
		return nil, err
	}
	getBlockResp := GetBlockResponse{}
	err = json.Unmarshal(body, &getBlockResp)
	if err != nil {
		return nil, err
	}
	return getBlockResp.Result, nil
}

func (c *Client) GetLatestBlock(ctx context.Context) (*Block, error) {
	payload := preparePayload("eth_getBlockByNumber", []interface{}{"latest", "true"})
	body, err := c.postRequest(ctx, payload)
	if err != nil {
		return nil, err
	}
	getBlockResp := GetBlockResponse{}
	err = json.Unmarshal(body, &getBlockResp)
	if err != nil {
		return nil, err
	}
	return getBlockResp.Result, nil
}

// GetBundleName returns the bundle name by a specific block number
func (c *Client) GetBundleName(ctx context.Context, blockNum uint64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.blockArchiverHost+fmt.Sprintf("/bsc/v1/blocks/%d/bundle/name", blockNum), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("failed to get bundle name")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	getBundleNameResp := GetBundleNameResponse{}
	err = json.Unmarshal(body, &getBundleNameResp)
	if err != nil {
		return "", err
	}
	return getBundleNameResp.Data, nil
}

// GetBundleBlocksByBlockNum returns the bundle blocks by block number that within the range
func (c *Client) GetBundleBlocksByBlockNum(ctx context.Context, blockNum uint64) ([]*Block, error) {
	payload := preparePayload("eth_getBundledBlockByNumber", []interface{}{Int64ToHex(int64(blockNum))})
	body, err := c.postRequest(ctx, payload)
	if err != nil {
		return nil, err
	}
	getBlocksResp := GetBlocksResponse{}
	err = json.Unmarshal(body, &getBlocksResp)
	if err != nil {
		return nil, err
	}
	return getBlocksResp.Result, nil
}

// GetBundleBlocks returns the bundle blocks by object name
func (c *Client) GetBundleBlocks(ctx context.Context, objectName string) ([]*Block, error) {
	var urlStr string
	parts := strings.Split(c.spHost, "//")
	urlStr = parts[0] + "//" + c.bucketName + "." + parts[1] + "/" + objectName

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	tempFile, err := os.CreateTemp("", "bundle")
	if err != nil {
		fmt.Printf("Failed to create temporary file: %v\n", err)
		return nil, err
	}
	defer os.Remove(tempFile.Name())
	// Write the content to the temporary file
	_, err = tempFile.Write(body)
	if err != nil {
		fmt.Printf("Failed to write downloaded bundle to file: %v\n", err)
		return nil, err
	}
	defer tempFile.Close()

	bundleObjects, err := bundlesdk.NewBundleFromFile(tempFile.Name())
	if err != nil {
		fmt.Printf("Failed to create bundle from file: %v\n", err)
		return nil, err
	}
	var blocksInfo []*Block
	for _, objMeta := range bundleObjects.GetBundleObjectsMeta() {
		objFile, _, err := bundleObjects.GetObject(objMeta.Name)
		if err != nil {
			return nil, err
		}

		var objectInfo []byte
		objectInfo, err = io.ReadAll(objFile)
		if err != nil {
			objFile.Close()
			return nil, err
		}
		objFile.Close()

		var blockInfo *Block
		err = json.Unmarshal(objectInfo, &blockInfo)
		if err != nil {
			return nil, err
		}
		blocksInfo = append(blocksInfo, blockInfo)
	}

	return blocksInfo, nil
}

// postRequest sends a POST request to the block archiver service
func (c *Client) postRequest(ctx context.Context, payload map[string]interface{}) ([]byte, error) {
	// Encode payload to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// post call to block archiver
	req, err := http.NewRequestWithContext(ctx, "POST", c.blockArchiverHost, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Perform the HTTP request
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("failed to get response")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// preparePayload prepares the payload for the request
func preparePayload(method string, params []interface{}) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
}

package jimeng

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common/volcsign"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

// SignRequestForJimeng 对即梦 API 请求进行签名，支持 http.Request 或 header+url+body 方式
//func SignRequestForJimeng(req *http.Request, accessKey, secretKey string) error {
//	var bodyBytes []byte
//	var err error
//
//	if req.Body != nil {
//		bodyBytes, err = io.ReadAll(req.Body)
//		if err != nil {
//			return fmt.Errorf("read request body failed: %w", err)
//		}
//		_ = req.Body.Close()
//		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // rewind
//	} else {
//		bodyBytes = []byte{}
//	}
//
//	return signJimengHeaders(&req.Header, req.Method, req.URL, bodyBytes, accessKey, secretKey)
//}

const HexPayloadHashKey = "HexPayloadHash"

func SetPayloadHash(c *gin.Context, req any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	logger.LogInfo(c, fmt.Sprintf("SetPayloadHash body: %s", body))
	payloadHash := sha256.Sum256(body)
	hexPayloadHash := hex.EncodeToString(payloadHash[:])
	c.Set(HexPayloadHashKey, hexPayloadHash)
	return nil
}
func getPayloadHash(c *gin.Context) string {
	return c.GetString(HexPayloadHashKey)
}

func Sign(c *gin.Context, req *http.Request, apiKey string) error {
	var bodyBytes []byte
	var err error

	if req.Body != nil {
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return err
		}
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Rewind
	}

	keyParts := strings.Split(apiKey, "|")
	if len(keyParts) != 2 {
		return errors.New("invalid api key format for jimeng: expected 'ak|sk'")
	}
	accessKey := strings.TrimSpace(keyParts[0])
	secretKey := strings.TrimSpace(keyParts[1])

	// jimeng 始终参与签名 content-type，缺省补 application/json 以保持历史行为。
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// 即梦走火山引擎 cv 服务，region=cn-north-1。
	return volcsign.SignRequest(req, bodyBytes, accessKey, secretKey, "cn-north-1", "cv")
}

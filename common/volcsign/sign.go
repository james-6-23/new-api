// Package volcsign implements Volcengine / BytePlus top-level OpenAPI request
// signing (HMAC-SHA256, AWS-SigV4-style 4-stage key derivation).
//
// It is shared by every channel that talks to a Volcengine-family OpenAPI
// (jimeng uses region=cn-north-1/service=cv; BytePlus素材库 uses
// region=ap-southeast-1/service=ark). Only region/service/host differ — the
// canonical-request and string-to-sign construction is identical.
package volcsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// SignRequest signs an http.Request in place with the given credentials,
// region and service. bodyBytes must be the exact request body (use nil/empty
// for a body-less request). It sets the Host, X-Date, X-Content-Sha256 and
// Authorization headers. Content-Type, if already set on the request, is
// included in the signed headers.
//
// The caller is responsible for ensuring req.Body (if any) still carries the
// same bytes passed as bodyBytes when the request is sent.
func SignRequest(req *http.Request, bodyBytes []byte, accessKey, secretKey, region, service string) error {
	return signAt(req, bodyBytes, accessKey, secretKey, region, service, time.Now().UTC())
}

// signAt is the testable core: it takes an explicit timestamp so signatures
// are reproducible in unit tests.
func signAt(req *http.Request, bodyBytes []byte, accessKey, secretKey, region, service string, t time.Time) error {
	payloadHash := sha256.Sum256(bodyBytes)
	hexPayloadHash := hex.EncodeToString(payloadHash[:])

	xDate := t.Format("20060102T150405Z")
	shortDate := t.Format("20060102")

	host := req.URL.Host
	req.Header.Set("Host", host)
	req.Header.Set("X-Date", xDate)
	req.Header.Set("X-Content-Sha256", hexPayloadHash)

	// Sort and encode query parameters to create canonical query string.
	queryParams := req.URL.Query()
	sortedKeys := make([]string, 0, len(queryParams))
	for k := range queryParams {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)
	var queryParts []string
	for _, k := range sortedKeys {
		values := queryParams[k]
		sort.Strings(values)
		for _, v := range values {
			queryParts = append(queryParts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
		}
	}
	canonicalQueryString := strings.Join(queryParts, "&")

	headersToSign := map[string]string{
		"host":             host,
		"x-date":           xDate,
		"x-content-sha256": hexPayloadHash,
	}
	if req.Header.Get("Content-Type") != "" {
		headersToSign["content-type"] = req.Header.Get("Content-Type")
	}

	var signedHeaderKeys []string
	for k := range headersToSign {
		signedHeaderKeys = append(signedHeaderKeys, k)
	}
	sort.Strings(signedHeaderKeys)

	var canonicalHeaders strings.Builder
	for _, k := range signedHeaderKeys {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(headersToSign[k]))
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(signedHeaderKeys, ";")

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		req.URL.Path,
		canonicalQueryString,
		canonicalHeaders.String(),
		signedHeaders,
		hexPayloadHash,
	)

	hashedCanonicalRequest := sha256.Sum256([]byte(canonicalRequest))
	hexHashedCanonicalRequest := hex.EncodeToString(hashedCanonicalRequest[:])

	credentialScope := fmt.Sprintf("%s/%s/%s/request", shortDate, region, service)
	stringToSign := fmt.Sprintf("HMAC-SHA256\n%s\n%s\n%s",
		xDate,
		credentialScope,
		hexHashedCanonicalRequest,
	)

	kDate := hmacSHA256([]byte(secretKey), []byte(shortDate))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	authorization := fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey,
		credentialScope,
		signedHeaders,
		signature,
	)
	req.Header.Set("Authorization", authorization)
	return nil
}

// hmacSHA256 computes HMAC-SHA256(key, data).
func hmacSHA256(key []byte, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

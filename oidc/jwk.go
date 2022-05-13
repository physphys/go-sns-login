package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/url"
	"time"
)

type jwks struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	E   string `json:"e"`
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	Alg string `json:"alg"`
}

func (token idToken) validateSignature(jwksUrl string) error {
	key, err := token.getJwk(jwksUrl)
	if err != nil {
		return err
	}

	byteN, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return fmt.Errorf("failed to decode base64 modulus: %w", err)
	}

	const standardExponent = 65537
	pubKey := &rsa.PublicKey{
		N: new(big.Int).SetBytes(byteN),
		E: standardExponent, // TODO: key.E -> "AQAB"から導きたい
	}

	headerAndPayload := fmt.Sprintf("%s.%s", token.rawHeader, token.RawPayload)
	sha := sha256.New()
	sha.Write([]byte(headerAndPayload))

	decSignature, err := base64.RawURLEncoding.DecodeString(token.rawSignature)
	if err != nil {
		return fmt.Errorf("failed to base64 decode id_token signature: %w", err)
	}

	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, sha.Sum(nil), decSignature); err != nil {
		return fmt.Errorf("failed to verify id_token signature: %w", err)
	}

	return nil
}

func (token idToken) getJwk(jwksUrl string) (jwk, error) {
	parsedUrl, err := url.Parse(jwksUrl)
	if err != nil {
		return jwk{}, fmt.Errorf("failed to parse jwks url: %w", err)
	}

	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), httpTimeoutSec*time.Second)
	defer cancel()
	reqWithCtx, err := http.NewRequestWithContext(ctxWithTimeout, http.MethodGet, parsedUrl.String(), nil)
	if err != nil {
		return jwk{}, fmt.Errorf("failed to create request of GET JWKs endpoint: %w", err)
	}

	httpClient := &http.Client{}
	resp, err := httpClient.Do(reqWithCtx)
	if err != nil {
		return jwk{}, fmt.Errorf("failed to GET JWKs endpoint: %w", err)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(resp.Body)
	byteArray, _ := ioutil.ReadAll(resp.Body)

	keys := &jwks{}
	if err := json.Unmarshal(byteArray, keys); err != nil {
		return jwk{}, fmt.Errorf("failed to unmarshal JWKs response: %w", err)
	}

	foundKey, err := keys.find(token.header.Kid)
	if err != nil {
		return jwk{}, err
	}

	return foundKey, nil
}

func (keys jwks) find(kid string) (jwk, error) {
	var foundKey jwk
	for _, key := range keys.Keys {
		if key.Kid == kid {
			foundKey = key

			break
		}
	}

	if foundKey != (jwk{}) {
		return foundKey, nil
	} else {
		return jwk{}, errJwkNotFound
	}
}

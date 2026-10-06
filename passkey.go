package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// passkey is a WebAuthn credential exported from a Bitwarden/Vaultwarden vault
// (login.fido2Credentials[0]), used as a software authenticator.
type passkey struct {
	CredentialID string `json:"credentialId"` // GUID (Bitwarden) or base64url
	KeyValue     string `json:"keyValue"`     // base64 PKCS#8 ES256 private key
	RpID         string `json:"rpId"`
	UserHandle   string `json:"userHandle"` // base64url

	key   *ecdsa.PrivateKey
	rawID []byte
}

var guidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func decodeB64Any(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	s = strings.NewReplacer("+", "-", "/", "_").Replace(s)
	return base64.RawURLEncoding.DecodeString(s)
}

// parsePasskey parses the C411_PASSKEY JSON blob.
func parsePasskey(raw string) (*passkey, error) {
	p := &passkey{}
	if err := json.Unmarshal([]byte(raw), p); err != nil {
		return nil, fmt.Errorf("invalid C411_PASSKEY json: %w", err)
	}
	der, err := decodeB64Any(p.KeyValue)
	if err != nil {
		return nil, fmt.Errorf("invalid passkey keyValue: %w", err)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("invalid passkey private key: %w", err)
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("passkey private key is not ECDSA (ES256 expected)")
	}
	p.key = ec
	if guidRe.MatchString(p.CredentialID) {
		p.rawID, err = hex.DecodeString(strings.ReplaceAll(p.CredentialID, "-", ""))
	} else {
		p.rawID, err = decodeB64Any(p.CredentialID)
	}
	if err != nil || len(p.rawID) == 0 {
		return nil, fmt.Errorf("invalid passkey credentialId")
	}
	if p.RpID == "" {
		return nil, fmt.Errorf("passkey rpId missing")
	}
	return p, nil
}

// assertion builds the JSON "response" expected by SimpleWebAuthn's startAuthentication().
func (p *passkey) assertion(challenge, origin string) (map[string]any, error) {
	clientData, _ := json.Marshal(map[string]any{
		"type":        "webauthn.get",
		"challenge":   challenge,
		"origin":      origin,
		"crossOrigin": false,
	})

	rpHash := sha256.Sum256([]byte(p.RpID))
	// flags: UP(0x01) | UV(0x04) | BE(0x08) | BS(0x10) = synced, user-verified passkey
	authData := append(rpHash[:], 0x1d)
	authData = binary.BigEndian.AppendUint32(authData, 0)

	cdHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, p.key, digest[:])
	if err != nil {
		return nil, fmt.Errorf("failed to sign assertion: %w", err)
	}

	return map[string]any{
		"id":    b64url(p.rawID),
		"rawId": b64url(p.rawID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64url(clientData),
			"authenticatorData": b64url(authData),
			"signature":         b64url(sig),
			"userHandle":        p.UserHandle,
		},
		"clientExtensionResults":  map[string]any{},
		"authenticatorAttachment": "platform",
	}, nil
}

// postJSON sends a JSON POST with the stored __csrf cookie and csrf-token header.
func (c *C411Client) postJSON(url string, body any) ([]byte, *http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	c.mu.RLock()
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}
	token := c.csrfToken
	c.mu.RUnlock()

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Referer", c.loginURL)
	req.Header.Set("csrf-token", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp, nil
}

// PasskeyLogin authenticates with the configured passkey (no Turnstile needed).
func (c *C411Client) PasskeyLogin() error {
	if c.passkey == nil {
		return fmt.Errorf("no passkey configured")
	}
	if err := c.FirstRequest(); err != nil {
		return fmt.Errorf("first request failed: %w", err)
	}

	b, resp, err := c.postJSON(c.baseURL+"/api/auth/passkey-login-options", map[string]any{})
	if err != nil {
		return fmt.Errorf("passkey options request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("passkey options failed: %s: %s", resp.Status, truncate(b))
	}
	var opts struct {
		Options struct {
			Challenge string `json:"challenge"`
			RpID      string `json:"rpId"`
		} `json:"options"`
		ChallengeToken string `json:"challengeToken"`
	}
	if err := json.Unmarshal(b, &opts); err != nil || opts.Options.Challenge == "" {
		return fmt.Errorf("unexpected passkey options response: %s", truncate(b))
	}

	assertion, err := c.passkey.assertion(opts.Options.Challenge, c.baseURL)
	if err != nil {
		return err
	}
	b, resp, err = c.postJSON(c.baseURL+"/api/auth/passkey-login", map[string]any{
		"challengeToken": opts.ChallengeToken,
		"response":       assertion,
	})
	if err != nil {
		return fmt.Errorf("passkey login request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("passkey login failed: %s: %s", resp.Status, truncate(b))
	}

	c.mu.Lock()
	c.cookies = append(c.cookies, resp.Cookies()...)
	c.mu.Unlock()
	fmt.Println("[auth] Authenticated with passkey")
	return nil
}

func truncate(b []byte) string {
	if len(b) > 300 {
		b = b[:300]
	}
	return string(b)
}

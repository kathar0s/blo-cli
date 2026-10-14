// Package crypto reproduces Billing-On's client-side request encryption so the
// CLI can talk to the same endpoints the web SPA uses.
//
// Two schemes are involved (see ANALYSIS.md §5):
//
//   - AES-128-CBC over every *Ajax.do request body ("encBody"). The key is a
//     static value baked into the site's JS; a fresh random IV is prepended to
//     the ciphertext and the whole thing is hex-encoded.
//   - RSA PKCS#1 v1.5 over the login password, using a per-login public key the
//     server hands out.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
)

// keyCode is the static seed hardcoded in the site's login.html
// (`var keyCode = "etskey2024090314"`). The AES key is the first 32 hex
// characters of SHA1(keyCode), interpreted as hex -> 16 bytes (AES-128).
const keyCode = "etskey2024090314"

// aesKey derives the 16-byte AES key exactly as the JS does:
//
//	CryptoJS.enc.Hex.parse(CryptoJS.SHA1(keyCode).toString().substring(0,32))
func aesKey() []byte {
	sum := sha1.Sum([]byte(keyCode))
	full := hex.EncodeToString(sum[:]) // 40 hex chars
	key, _ := hex.DecodeString(full[:32])
	return key // 16 bytes
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	n := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+n)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(n)
	}
	return out
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("crypto: invalid padded length")
	}
	n := int(data[len(data)-1])
	if n == 0 || n > blockSize || n > len(data) {
		return nil, errors.New("crypto: invalid PKCS7 padding")
	}
	for _, b := range data[len(data)-n:] {
		if int(b) != n {
			return nil, errors.New("crypto: invalid PKCS7 padding")
		}
	}
	return data[:len(data)-n], nil
}

// EncBody encrypts a JSON parameter string into the hex blob the server expects
// as the value of the "encBody" form field. Output = hex(iv || ciphertext).
func EncBody(paramJSON string) (string, error) {
	block, err := aes.NewCipher(aesKey())
	if err != nil {
		return "", err
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	plain := pkcs7Pad([]byte(paramJSON), aes.BlockSize)
	ct := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, plain)
	out := make([]byte, 0, len(iv)+len(ct))
	out = append(out, iv...)
	out = append(out, ct...)
	return hex.EncodeToString(out), nil
}

// DecBody reverses EncBody. Used by tests and for inspecting captured traffic.
func DecBody(encHex string) (string, error) {
	raw, err := hex.DecodeString(encHex)
	if err != nil {
		return "", err
	}
	if len(raw) <= aes.BlockSize {
		return "", errors.New("crypto: ciphertext too short")
	}
	block, err := aes.NewCipher(aesKey())
	if err != nil {
		return "", err
	}
	iv, ct := raw[:aes.BlockSize], raw[aes.BlockSize:]
	if len(ct)%aes.BlockSize != 0 {
		return "", errors.New("crypto: ciphertext not block-aligned")
	}
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)
	unpadded, err := pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return "", err
	}
	return string(unpadded), nil
}

// RSAEncryptPassword encrypts the password with the server-provided public key
// (modulus and exponent given as hex strings, matching jsbn's rsa.setPublic).
// Output is a lowercase hex string, like jsbn's RSAKey.encrypt.
func RSAEncryptPassword(modulusHex, exponentHex, password string) (string, error) {
	mod := new(big.Int)
	if _, ok := mod.SetString(modulusHex, 16); !ok {
		return "", fmt.Errorf("crypto: bad RSA modulus hex")
	}
	exp := new(big.Int)
	if _, ok := exp.SetString(exponentHex, 16); !ok {
		return "", fmt.Errorf("crypto: bad RSA exponent hex")
	}
	pub := &rsa.PublicKey{N: mod, E: int(exp.Int64())}
	enc, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(password))
	if err != nil {
		return "", err
	}
	// Left-pad to the modulus byte length so the hex width matches the JS output.
	size := (pub.N.BitLen() + 7) / 8
	if len(enc) < size {
		padded := make([]byte, size)
		copy(padded[size-len(enc):], enc)
		enc = padded
	}
	return hex.EncodeToString(enc), nil
}

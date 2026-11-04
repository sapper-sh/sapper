package report

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// sigPrefix separates report signatures from any other use of the same key.
const sigPrefix = "sapper-report-signature-v1\n"

// KeyID is a short fingerprint of a public key: the first 16 hex characters of its sha256.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// Sign signs the report hash with key. Seal must have run first.
func (r *Report) Sign(key ed25519.PrivateKey) error {
	if r.Hash == "" {
		return errors.New("report is not sealed")
	}
	pub := key.Public().(ed25519.PublicKey)
	r.Signature = &Signature{
		Alg:       "ed25519",
		KeyID:     KeyID(pub),
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(sigPrefix+r.Hash))),
	}
	return nil
}

// CheckSignature verifies the report's signature. With want set, the report must be
// signed by that key; otherwise the embedded public key is used, which proves only
// that the report was not changed after signing, not who signed it.
func (r *Report) CheckSignature(want ed25519.PublicKey) error {
	if r.Signature == nil {
		if want != nil {
			return errors.New("not signed")
		}
		return nil
	}
	s := r.Signature
	if s.Alg != "ed25519" {
		return fmt.Errorf("unknown signature algorithm %q", Clean(s.Alg))
	}
	pub, err := base64.StdEncoding.DecodeString(s.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("malformed public key")
	}
	if want != nil && !want.Equal(ed25519.PublicKey(pub)) {
		return fmt.Errorf("signed by key %s, expected %s", KeyID(pub), KeyID(want))
	}
	if KeyID(pub) != s.KeyID {
		return errors.New("key ID does not match the public key")
	}
	sig, err := base64.StdEncoding.DecodeString(s.Value)
	if err != nil || !ed25519.Verify(pub, []byte(sigPrefix+r.Hash), sig) {
		return errors.New("signature does not match")
	}
	return nil
}

const (
	pemPrivate = "PRIVATE KEY"
	pemPublic  = "PUBLIC KEY"
	maxKeyFile = 64 << 10
)

// GenerateKey writes a new ed25519 key pair: the private key to privPath (mode 0600,
// never overwritten) and the public key to privPath+".pub". It returns the public key.
func GenerateKey(privPath string, random io.Reader) (ed25519.PublicKey, string, error) {
	if random == nil {
		random = rand.Reader
	}
	pub, priv, err := ed25519.GenerateKey(random)
	if err != nil {
		return nil, "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, "", err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, "", err
	}
	pubPath := privPath + ".pub"
	for _, p := range []string{privPath, pubPath} {
		if _, err := os.Lstat(p); err == nil {
			return nil, "", fmt.Errorf("%s already exists; sapper never overwrites a key", p)
		}
	}
	if err := os.MkdirAll(filepath.Dir(privPath), 0o700); err != nil {
		return nil, "", err
	}
	if err := writeExclusive(privPath, pem.EncodeToMemory(&pem.Block{Type: pemPrivate, Bytes: der}), 0o600); err != nil {
		return nil, "", err
	}
	if err := writeExclusive(pubPath, pem.EncodeToMemory(&pem.Block{Type: pemPublic, Bytes: pubDER}), 0o644); err != nil {
		return nil, "", err
	}
	return pub, pubPath, nil
}

// LoadPrivateKey reads an ed25519 private key written by GenerateKey. The returned
// warning is non-empty when the file is readable by other users. Errors never include
// the file's contents.
func LoadPrivateKey(path string) (ed25519.PrivateKey, string, error) {
	data, warn, err := readKeyFile(path)
	if err != nil {
		return nil, "", err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != pemPrivate {
		return nil, "", fmt.Errorf("%s: not a PEM %q block; create one with `sapper keygen`", path, pemPrivate)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, "", fmt.Errorf("%s: cannot parse private key", path)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, "", fmt.Errorf("%s: not an ed25519 key", path)
	}
	return priv, warn, nil
}

// LoadPublicKey reads an ed25519 public key in PEM form.
func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	data, _, err := readKeyFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != pemPublic {
		return nil, fmt.Errorf("%s: not a PEM %q block", path, pemPublic)
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: cannot parse public key", path)
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an ed25519 key", path)
	}
	return pub, nil
}

func readKeyFile(path string) ([]byte, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	if !st.Mode().IsRegular() {
		return nil, "", fmt.Errorf("%s: not a regular file", path)
	}
	var warn string
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
		warn = fmt.Sprintf("key file %s is readable by other users (mode %v); run chmod 600 on it", path, st.Mode().Perm())
	}
	data, err := io.ReadAll(io.LimitReader(f, maxKeyFile+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxKeyFile {
		return nil, "", fmt.Errorf("%s: too large for a key file", path)
	}
	return data, warn, nil
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}

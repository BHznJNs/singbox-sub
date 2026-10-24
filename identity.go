package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

type identity struct {
	Version    int    `json:"version"`
	UUID       string `json:"uuid"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	ShortID    string `json:"short_id"`
	Token      string `json:"token"`
}

func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newIdentity() (identity, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return identity{}, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return identity{}, err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
	shortID, err := randomHex(8)
	if err != nil {
		return identity{}, err
	}
	token, err := randomHex(32)
	if err != nil {
		return identity{}, err
	}
	return identity{1, uuid, base64.RawURLEncoding.EncodeToString(key.Bytes()),
		base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), shortID, token}, nil
}

func (id identity) validate() error {
	if id.Version != 1 || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id.UUID) {
		return errors.New("invalid identity version or UUID")
	}
	for _, field := range []struct {
		value  string
		length int
	}{{id.ShortID, 16}, {id.Token, 64}} {
		if len(field.value) != field.length {
			return errors.New("invalid identity short ID or token")
		}
		if _, err := hex.DecodeString(field.value); err != nil {
			return errors.New("invalid identity short ID or token")
		}
	}
	b, err := base64.RawURLEncoding.DecodeString(id.PrivateKey)
	if err != nil {
		return errors.New("invalid identity private key")
	}
	key, err := ecdh.X25519().NewPrivateKey(b)
	if err != nil || base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()) != id.PublicKey {
		return errors.New("invalid identity key pair")
	}
	return nil
}

func atomicWrite(path string, content []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(content); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func loadIdentity(dataDir string) (identity, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return identity{}, err
	}
	dir := filepath.Join(dataDir, "identity")
	path := filepath.Join(dir, "state.json")
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(dataDir)
		if err != nil {
			return identity{}, err
		}
		for _, entry := range entries {
			if entry.Name() != "lost+found" {
				return identity{}, errors.New("identity is missing in nonempty /data; refusing to replace it")
			}
		}
	}
	if err := os.Mkdir(dir, 0700); err == nil {
		id, err := newIdentity()
		if err != nil {
			return identity{}, err
		}
		content, err := json.MarshalIndent(id, "", "  ")
		if err != nil {
			return identity{}, err
		}
		if err := atomicWrite(path, content); err != nil {
			return identity{}, err
		}
		return id, nil
	} else if !errors.Is(err, os.ErrExist) {
		return identity{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return identity{}, fmt.Errorf("existing identity is missing or unreadable; refusing to replace it: %w", err)
	}
	var id identity
	if err := json.Unmarshal(content, &id); err != nil {
		return identity{}, errors.New("existing identity is corrupt; refusing to replace it")
	}
	if err := id.validate(); err != nil {
		return identity{}, fmt.Errorf("existing identity is corrupt; refusing to replace it: %w", err)
	}
	return id, nil
}

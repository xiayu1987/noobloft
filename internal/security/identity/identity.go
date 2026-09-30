// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package identity

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/libp2p/go-libp2p/core/crypto"
)

const pskHeader = "/key/swarm/psk/1.0.0/"

const pskCodec = "/base16/"

func LoadOrCreateIdentity(dir string) (crypto.PrivKey, error) {
	path := filepath.Join(dir, "identity.key")
	raw, err := os.ReadFile(path)
	if err == nil {
		priv, err := crypto.UnmarshalPrivateKey(raw)
		if err != nil {
			return nil, localization.Errorf("errors.identity.parseKey", path, err)
		}
		return priv, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, localization.Errorf("errors.identity.readKey", err)
	}

	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, localization.Errorf("errors.identity.generateKey", err)
	}
	encoded, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, localization.Errorf("errors.identity.encodeKey", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, localization.Errorf("errors.config.createDir", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return nil, localization.Errorf("errors.identity.writeKey", err)
	}
	return priv, nil
}

func GenerateSwarmKey(dir string) (string, error) {
	path := filepath.Join(dir, "swarm.key")
	if _, err := os.Stat(path); err == nil {
		return "", localization.Errorf("errors.identity.pskExists", path)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", localization.Errorf("errors.identity.generatePSK", err)
	}
	content := pskHeader + "\n" + pskCodec + "\n" + hex.EncodeToString(key) + "\n"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", localization.Errorf("errors.config.createDir", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", localization.Errorf("errors.identity.writePSK", err)
	}
	return path, nil
}

func LoadSwarmKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, "swarm.key")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, localization.Errorf("errors.identity.missingPSK", path, err)
		}
		return nil, localization.Errorf("errors.identity.readPSK", err)
	}
	lines := strings.Fields(strings.TrimSpace(string(raw)))
	if len(lines) != 3 {
		return nil, localization.Errorf("errors.identity.pskLines", len(lines))
	}
	if lines[0] != pskHeader {
		return nil, localization.Errorf("errors.identity.pskHeader", lines[0])
	}
	if lines[1] != pskCodec {
		return nil, localization.Errorf("errors.identity.pskEncoding", lines[1])
	}
	key, err := hex.DecodeString(lines[2])
	if err != nil {
		return nil, localization.Errorf("errors.identity.decodePSK", err)
	}
	if len(key) != 32 {
		return nil, localization.Errorf("errors.identity.pskLength", len(key))
	}
	return key, nil
}

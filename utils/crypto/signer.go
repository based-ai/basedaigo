// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"errors"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var errInvalidTLSKey = errors.New("invalid TLS key")

// MultiSigner supports the signing of multiple signature types
type MultiSigner interface {
	// SignBLS signs the byte representation of the unsigned ip with a bls key.
	SignBLS(msg []byte) []byte
	// SignTLS signs the byte representation of the unsigned ip with a tls key.
	// Returns an error if signing failed.
	SignTLS(msg []byte) ([]byte, error)
}

// TLSSigner is signs ips with a TLS key.
type TLSSigner struct {
	privateKey crypto.Signer
}

// NewTLSSigner returns a new instance of TLSSigner.
func NewTLSSigner(cert *tls.Certificate) (TLSSigner, error) {
	privateKey, ok := cert.PrivateKey.(crypto.Signer)
	if !ok {
		return TLSSigner{}, errInvalidTLSKey
	}

	return TLSSigner{
		privateKey: privateKey,
	}, nil
}

func (t TLSSigner) Sign(bytes []byte) ([]byte, error) {
	tlsSig, err := t.privateKey.Sign(rand.Reader,
		hashing.ComputeHash256(bytes), crypto.SHA256)
	if err != nil {
		return nil, err
	}

	return tlsSig, err
}

// BLSSigner signs ips with a BLS key.
type BLSSigner struct {
	secretKey *bls.SecretKey
}

// NewBLSSigner returns a new instance of BLSSigner.
func NewBLSSigner(secretKey *bls.SecretKey) BLSSigner {
	return BLSSigner{
		secretKey: secretKey,
	}
}

func (b BLSSigner) Sign(msg []byte) []byte {
	return bls.SignatureToBytes(bls.Sign(b.secretKey, msg))
}

type NoOpBLSSigner struct{}

func (NoOpBLSSigner) Sign([]byte) []byte {
	return nil
}

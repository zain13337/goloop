package consensus

import (
	"errors"

	"github.com/icon-project/goloop/common"
	"github.com/icon-project/goloop/common/crypto"
)

type byteser interface {
	bytes() []byte
}

// base class for signed data
type signedBase struct {
	// shall be initialized
	_byteser  byteser
	Signature common.Signature

	_hash      []byte
	_publicKey *crypto.PublicKey
}

func (s *signedBase) hash() []byte {
	if s._hash == nil {
		s._hash = crypto.SHA3Sum256(s._byteser.bytes())
	}
	return s._hash
}

func (s *signedBase) publicKey() *crypto.PublicKey {
	if s._publicKey == nil {
		publicKey, err := s.Signature.RecoverPublicKey(s.hash())
		if err != nil {
			return nil
		}
		s._publicKey = publicKey
	}
	return s._publicKey
}

func (s *signedBase) address() *common.Address {
	publicKey := s.publicKey()
	if publicKey == nil {
		return nil
	}
	return common.NewAccountAddressFromPublicKey(publicKey)
}

func (s *signedBase) verify() error {
	if s.publicKey() == nil {
		return errors.New("bad signature")
	}
	return nil
}

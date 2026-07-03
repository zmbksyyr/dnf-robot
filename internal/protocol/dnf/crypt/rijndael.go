package crypt

import "crypto/aes"

type RijndaelCipher struct {
	ciph interface {
		Encrypt(dst, src []byte)
		Decrypt(dst, src []byte)
	}
}

func NewRijndaelCipher() *RijndaelCipher {
	return &RijndaelCipher{}
}

func (r *RijndaelCipher) SetKey(key []byte) error {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return ErrInvalidKeySize
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	r.ciph = blk.(interface {
		Encrypt(dst, src []byte)
		Decrypt(dst, src []byte)
	})
	return nil
}

func (r *RijndaelCipher) KeySize() int {
	return 16
}

func (r *RijndaelCipher) BlockSize() int {
	return 16
}

func (r *RijndaelCipher) Encrypt(data []byte) ([]byte, error) {
	if r.ciph == nil {
		return nil, ErrNotInitialized
	}
	blockSize := r.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		r.ciph.Encrypt(out[i:i+blockSize], data[i:i+blockSize])
	}
	return out, nil
}

func (r *RijndaelCipher) Decrypt(data []byte) ([]byte, error) {
	if r.ciph == nil {
		return nil, ErrNotInitialized
	}
	blockSize := r.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		r.ciph.Decrypt(out[i:i+blockSize], data[i:i+blockSize])
	}
	return out, nil
}

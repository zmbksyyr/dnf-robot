package crypt

// KASUMI block cipher (libtomcrypt-compatible implementation)
// Matches the C++ DNFCryptKasumi.cpp game server cipher exactly.

type KasumiCipher struct {
	KLi1 [8]uint16
	KLi2 [8]uint16
	KOi1 [8]uint16
	KOi2 [8]uint16
	KOi3 [8]uint16
	KIi1 [8]uint16
	KIi2 [8]uint16
	KIi3 [8]uint16
}

var kasumiS7 = [128]uint16{
	54, 50, 62, 56, 22, 34, 94, 96, 38, 6, 63, 93, 2, 18, 123, 33,
	55, 113, 39, 114, 21, 67, 65, 12, 47, 73, 46, 27, 25, 111, 124, 81,
	53, 9, 121, 79, 52, 60, 58, 48, 101, 127, 40, 120, 104, 70, 71, 43,
	20, 122, 72, 61, 23, 109, 13, 100, 77, 1, 16, 7, 82, 10, 105, 98,
	117, 116, 76, 11, 89, 106, 0, 125, 118, 99, 86, 69, 30, 57, 126, 87,
	112, 51, 17, 5, 95, 14, 90, 84, 91, 8, 35, 103, 32, 97, 28, 66,
	102, 31, 26, 45, 75, 4, 85, 92, 37, 74, 80, 49, 68, 29, 115, 44,
	64, 107, 108, 24, 110, 83, 36, 78, 42, 19, 15, 41, 88, 119, 59, 3,
}

var kasumiS9 = [512]uint16{
	167, 239, 161, 379, 391, 334, 9, 338, 38, 226, 48, 358, 452, 385, 90, 397,
	183, 253, 147, 331, 415, 340, 51, 362, 306, 500, 262, 82, 216, 159, 356, 177,
	175, 241, 489, 37, 206, 17, 0, 333, 44, 254, 378, 58, 143, 220, 81, 400,
	95, 3, 315, 245, 54, 235, 218, 405, 472, 264, 172, 494, 371, 290, 399, 76,
	165, 197, 395, 121, 257, 480, 423, 212, 240, 28, 462, 176, 406, 507, 288, 223,
	501, 407, 249, 265, 89, 186, 221, 428, 164, 74, 440, 196, 458, 421, 350, 163,
	232, 158, 134, 354, 13, 250, 491, 142, 191, 69, 193, 425, 152, 227, 366, 135,
	344, 300, 276, 242, 437, 320, 113, 278, 11, 243, 87, 317, 36, 93, 496, 27,
	487, 446, 482, 41, 68, 156, 457, 131, 326, 403, 339, 20, 39, 115, 442, 124,
	475, 384, 508, 53, 112, 170, 479, 151, 126, 169, 73, 268, 279, 321, 168, 364,
	363, 292, 46, 499, 393, 327, 324, 24, 456, 267, 157, 460, 488, 426, 309, 229,
	439, 506, 208, 271, 349, 401, 434, 236, 16, 209, 359, 52, 56, 120, 199, 277,
	465, 416, 252, 287, 246, 6, 83, 305, 420, 345, 153, 502, 65, 61, 244, 282,
	173, 222, 418, 67, 386, 368, 261, 101, 476, 291, 195, 430, 49, 79, 166, 330,
	280, 383, 373, 128, 382, 408, 155, 495, 367, 388, 274, 107, 459, 417, 62, 454,
	132, 225, 203, 316, 234, 14, 301, 91, 503, 286, 424, 211, 347, 307, 140, 374,
	35, 103, 125, 427, 19, 214, 453, 146, 498, 314, 444, 230, 256, 329, 198, 285,
	50, 116, 78, 410, 10, 205, 510, 171, 231, 45, 139, 467, 29, 86, 505, 32,
	72, 26, 342, 150, 313, 490, 431, 238, 411, 325, 149, 473, 40, 119, 174, 355,
	185, 233, 389, 71, 448, 273, 372, 55, 110, 178, 322, 12, 469, 392, 369, 190,
	1, 109, 375, 137, 181, 88, 75, 308, 260, 484, 98, 272, 370, 275, 412, 111,
	336, 318, 4, 504, 492, 259, 304, 77, 337, 435, 21, 357, 303, 332, 483, 18,
	47, 85, 25, 497, 474, 289, 100, 269, 296, 478, 270, 106, 31, 104, 433, 84,
	414, 486, 394, 96, 99, 154, 511, 148, 413, 361, 409, 255, 162, 215, 302, 201,
	266, 351, 343, 144, 441, 365, 108, 298, 251, 34, 182, 509, 138, 210, 335, 133,
	311, 352, 328, 141, 396, 346, 123, 319, 450, 281, 429, 228, 443, 481, 92, 404,
	485, 422, 248, 297, 23, 213, 130, 466, 22, 217, 283, 70, 294, 360, 419, 127,
	312, 377, 7, 468, 194, 2, 117, 295, 463, 258, 224, 447, 247, 187, 80, 398,
	284, 353, 105, 390, 299, 471, 470, 184, 57, 200, 348, 63, 204, 188, 33, 451,
	97, 30, 310, 219, 94, 160, 129, 493, 64, 179, 263, 102, 189, 207, 114, 402,
	438, 477, 387, 122, 192, 42, 381, 5, 145, 118, 180, 449, 293, 323, 136, 380,
	43, 66, 60, 455, 341, 445, 202, 432, 8, 237, 15, 376, 436, 464, 59, 461,
}

func NewKasumiCipher() *KasumiCipher {
	return &KasumiCipher{}
}

// ROL16 matches C++ ROL16 macro: ((x<<y) | (x>>(16-y))) & 0xFFFF
func kasumiROL16(x uint16, y int) uint16 {
	return ((x << y) | (x >> (16 - y))) & 0xFFFF
}

func (k *KasumiCipher) SetKey(key []byte) error {
	if len(key) != 16 {
		return ErrInvalidKeySize
	}

	// [C++->Go] libtomcrypt KASUMI key schedule with hardcoded C[] constants
	// NOT the 3GPP cascaded-C function key schedule
	C := [8]uint16{0x0123, 0x4567, 0x89AB, 0xCDEF, 0xFEDC, 0xBA98, 0x7654, 0x3210}
	var ukey [8]uint16
	var Kprime [8]uint16

	for n := 0; n < 8; n++ {
		ukey[n] = (uint16(key[2*n]) << 8) | uint16(key[2*n+1])
	}

	for n := 0; n < 8; n++ {
		Kprime[n] = ukey[n] ^ C[n]
	}

	for n := 0; n < 8; n++ {
		k.KLi1[n] = kasumiROL16(ukey[n], 1)
		k.KLi2[n] = Kprime[(n+2)&0x7]
		k.KOi1[n] = kasumiROL16(ukey[(n+1)&0x7], 5)
		k.KOi2[n] = kasumiROL16(ukey[(n+5)&0x7], 8)
		k.KOi3[n] = kasumiROL16(ukey[(n+6)&0x7], 13)
		k.KIi1[n] = Kprime[(n+4)&0x7]
		k.KIi2[n] = Kprime[(n+3)&0x7]
		k.KIi3[n] = Kprime[(n+7)&0x7]
	}

	return nil
}

func (k *KasumiCipher) KeySize() int {
	return 16
}

func (k *KasumiCipher) BlockSize() int {
	return 8
}

// [C++->Go] libtomcrypt KASUMI FI function - single 16-bit subkey version
func kasumiFI(in uint16, subkey uint16) uint16 {
	nine := (in >> 7) & 0x1FF
	seven := in & 0x7F

	nine = kasumiS9[nine] ^ seven
	seven = kasumiS7[seven] ^ (nine & 0x7F)
	seven ^= (subkey >> 9)
	nine ^= (subkey & 0x1FF)
	nine = kasumiS9[nine] ^ seven
	seven = kasumiS7[seven] ^ (nine & 0x7F)

	return (seven << 9) | nine
}

// [C++->Go] libtomcrypt KASUMI FO function
func (k *KasumiCipher) kasumiFO(in uint32, round int) uint32 {
	left := uint16(in >> 16)
	right := uint16(in & 0xFFFF)

	left ^= k.KOi1[round]
	left = kasumiFI(left, k.KIi1[round])
	left ^= right

	right ^= k.KOi2[round]
	right = kasumiFI(right, k.KIi2[round])
	right ^= left

	left ^= k.KOi3[round]
	left = kasumiFI(left, k.KIi3[round])
	left ^= right

	return (uint32(right) << 16) | uint32(left)
}

// [C++->Go] libtomcrypt KASUMI FL function
func (k *KasumiCipher) kasumiFL(in uint32, round int) uint32 {
	l := uint16(in >> 16)
	r := uint16(in & 0xFFFF)

	a := l & k.KLi1[round]
	r ^= kasumiROL16(a, 1)

	b := r | k.KLi2[round]
	l ^= kasumiROL16(b, 1)

	return (uint32(l) << 16) | uint32(r)
}

func (k *KasumiCipher) Encrypt(data []byte) ([]byte, error) {
	blockSize := k.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		left := loadU32BE(data[i : i+4])
		right := loadU32BE(data[i+4 : i+8])

		for n := 0; n <= 7; {
			temp := k.kasumiFL(left, n)
			temp = k.kasumiFO(temp, n)
			n++
			right ^= temp

			temp = k.kasumiFO(right, n)
			temp = k.kasumiFL(temp, n)
			n++
			left ^= temp
		}

		storeU32BE(out[i:i+4], left)
		storeU32BE(out[i+4:i+8], right)
	}
	return out, nil
}

func (k *KasumiCipher) Decrypt(data []byte) ([]byte, error) {
	blockSize := k.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		left := loadU32BE(data[i : i+4])
		right := loadU32BE(data[i+4 : i+8])

		for n := 7; n >= 0; {
			temp := k.kasumiFO(right, n)
			temp = k.kasumiFL(temp, n)
			n--
			left ^= temp

			temp = k.kasumiFL(left, n)
			temp = k.kasumiFO(temp, n)
			n--
			right ^= temp
		}

		storeU32BE(out[i:i+4], left)
		storeU32BE(out[i+4:i+8], right)
	}
	return out, nil
}

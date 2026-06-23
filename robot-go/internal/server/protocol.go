package server

import (
	"bytes"
	"strconv"
	"strings"
)

var packHead = []byte("ZYSHDKXHGA")
var tailPos = []byte("LKJDSPHXZB")

var crcTab1 = [256]uint32{
	0xf3b97148, 0x206f85b3, 0xce61e49f, 0x79dcb8a4, 0xe0d5e91e, 0x97d2d988, 0x09b64c2b, 0x7eb17cbd,
	0x5edef90e, 0xb7bd5c3b, 0xc0ba6cad, 0x6906c2fe, 0xf762575d, 0x806567cb, 0x196c3671, 0x6e6b06e7,
	0xead54739, 0x9dd277af, 0x04db2615, 0xbe0b1010, 0xb966d409, 0x67dd4acc, 0xf9b9df6f, 0x8ebeeff9,
	0xe3630b12, 0x94643b84, 0x0d6d6a3e, 0xe40ecf0b, 0x9309ff9d, 0x0a00ae27, 0x7d079eb1, 0x29d9c998,
	0xf00f9344, 0x8708a3d2, 0x1e01f268, 0xedb88320, 0x9abfb3b6, 0x03b6e20c, 0x74b1d29a, 0x89d32be0,
	0xfed41b76, 0x10da7a5a, 0x17b7be43, 0x60b08ed5, 0x5768b525, 0x41047a60, 0xdf60efc3, 0xa867df55,
	0x076dc419, 0x706af48f, 0xe963a535, 0x9e6495a3, 0xb40bbe37, 0xc30c8ea1, 0x26d930ac, 0x51de003a,
	0x0edb8832, 0xe7b82d07, 0x90bf1d91, 0xb0d09822, 0xc7d7a8b4, 0x59b33d17, 0x2eb40d81, 0x7a6a5aa8,
	0x1db71064, 0x6ab020f2, 0x6ddde4eb, 0xf4d4b551, 0x83d385c7, 0x73dc1683, 0x5005713c, 0x270241aa,
	0x136c9856, 0x646ba8c0, 0xfd62f97a, 0x8a65c9ec, 0x14015c4f, 0x63066cd9, 0xfa0f3d63, 0x8d080df5,
	0xd6d6a3e8, 0xa1d1937e, 0x38d8c2c4, 0x4fdff252, 0xd1bb67f1, 0xa6bc5767, 0x3fb506dd, 0x48b2364b,
	0xd80d2bda, 0xaf0a1b4c, 0x84be41de, 0x1adad47d, 0xc90c2086, 0x36034af6, 0x316e8eef, 0x4669be79,
	0x88085ae6, 0xff0f6a70, 0x66063bca, 0x11010b5c, 0x8f659eff, 0xf862ae69, 0x616bffd3, 0x166ccf45,
	0xa00ae278, 0xd70dd2ee, 0x4e048354, 0x3903b3c2, 0xa7672661, 0xd06016f7, 0x4969474d, 0x3e6e77db,
	0xaed16a4a, 0xd9d65adc, 0x40df0b66, 0x37d83bf0, 0xa9bcae53, 0xdebb9ec5, 0x47b2cf7f, 0x30b5ffe9,
	0xbdbdf21c, 0xcabac28a, 0x00000000, 0x77073096, 0xbad03605, 0xcdd70693, 0x54de5729, 0x23d967bf,
	0xb3667a2e, 0xc4614ab8, 0x5d681b02, 0x2a6f2b94, 0xee0e612c, 0x990951ba, 0x53b39330, 0x24b4a3a6,
	0xc8d75180, 0xbfd06116, 0x21b4f4b5, 0x56b3c423, 0xcfba9599, 0xb8bda50f, 0x98d220bc, 0xefd5102a,
	0x2802b89e, 0x5f058808, 0xc60cd9b2, 0xb10be924, 0x2f6f7c87, 0x58684c11, 0xc1611dab, 0xb6662d3d,
	0x76dc4190, 0x01db7106, 0x71b18589, 0x06b6b51f, 0x9fbfe4a5, 0xe8b8d433, 0x7807c9a2, 0x0f00f934,
	0x5a05df1b, 0x2d02ef8d, 0x9609a88e, 0xe10e9818, 0x7f6a0dbb, 0x086d3d2d, 0x91646c97, 0xe6635c01,
	0x3b6e20c8, 0x4c69105e, 0xd56041e4, 0xa2677172, 0x3c03e4d1, 0x4b04d447, 0xd20d85fd, 0xa50ab56b,
	0x35b5a8fa, 0x42b2986c, 0xdbbbc9d6, 0xacbcf940, 0x32d86ce3, 0x45df5c75, 0xdcd60dcf, 0xabd13d59,
	0x6b6b51f4, 0x1c6c6162, 0x856530d8, 0xf262004e, 0x6c0695ed, 0x1b01a57b, 0x8208f4c1, 0xf50fc457,
	0x65b0d9c6, 0x12b7e950, 0x8cd37cf3, 0xfbd44c65, 0xbc66831a, 0x256fd2a0, 0x5268e236, 0x44042d73,
	0x4db26158, 0x3ab551ce, 0xa3bc0074, 0xd4bb30e2, 0x4adfa541, 0x3dd895d7, 0xa4d1c46d, 0xd3d6f4fb,
	0x0cb61b38, 0x92d28e9b, 0xe5d5be0d, 0xda60b8d0, 0x2bb45a92, 0x5cb36a04, 0xc2d7ffa7, 0xb5d0cf31,
	0xcb61b38c, 0xcc0c7795, 0xbb0b4703, 0x220216b9, 0x5505262f, 0x0bdbdf21, 0x81be16cd, 0xf6b9265b,
	0xc5ba3bbe, 0xb2bd0b28, 0x2cd99e8b, 0x5bdeae1d, 0x7cdcefb7, 0x33031de5, 0xaa0a4c5f, 0xdd0d7cc9,
	0x9b64c2b0, 0xec63f226, 0x756aa39c, 0x026d930a, 0x9c0906a9, 0xeb0e363f, 0x72076785, 0x05005713,
	0x95bf4a82, 0xe2b87a14, 0x7bb12bae, 0x6fb077e1, 0x18b74777, 0xad678846, 0x86d3d2d4, 0xf1d4e242,
	0x4369e96a, 0x346ed9fc, 0x68ddb3f8, 0x1fda836e, 0x8bbeb8ea, 0xfcb9887c, 0x62dd1ddf, 0x15da2d49,
}

var crcTab2 = [256]uint32{
	0xd6d6a3e8, 0xa1d1937e, 0x38d8c2c4, 0x4fdff252, 0xe0d5e91e, 0x97d2d988, 0x09b64c2b, 0x7eb17cbd,
	0xc90c2086, 0x36034af6, 0x316e8eef, 0x4669be79, 0x6c0695ed, 0x1b01a57b, 0x8208f4c1, 0xf50fc457,
	0x76dc4190, 0x01db7106, 0x71b18589, 0x06b6b51f, 0x2802b89e, 0xc60cd9b2, 0xb10be924, 0x9b64c2b0,
	0x9fbfe4a5, 0xe8b8d433, 0x7807c9a2, 0x0f00f934, 0xf3b97148, 0x206f85b3, 0xce61e49f, 0x79dcb8a4,
	0x7f6a0dbb, 0x086d3d2d, 0x91646c97, 0xe6635c01, 0xd80d2bda, 0xaf0a1b4c, 0x84be41de, 0x1adad47d,
	0x1db71064, 0x6ab020f2, 0x6ddde4eb, 0xf4d4b551, 0x0cb61b38, 0x92d28e9b, 0xe5d5be0d, 0xda60b8d0,
	0x9e6495a3, 0xb40bbe37, 0xc30c8ea1, 0x26d930ac, 0x51de003a, 0x5005713c, 0x270241aa, 0xa00ae278,
	0x9309ff9d, 0x0a00ae27, 0x5d681b02, 0x2a6f2b94, 0xf862ae69, 0x616bffd3, 0x166ccf45, 0x4369e96a,
	0x83d385c7, 0x73dc1683, 0xd70dd2ee, 0x4e048354, 0x3903b3c2, 0x3fb506dd, 0x48b2364b, 0x88085ae6,
	0x136c9856, 0x646ba8c0, 0xfd62f97a, 0x8a65c9ec, 0x14015c4f, 0x63066cd9, 0xfa0f3d63, 0x8d080df5,
	0xd1bb67f1, 0xa6bc5767, 0xff0f6a70, 0x66063bca, 0x11010b5c, 0x1fda836e, 0xcabac28a, 0x076dc419,
	0x3b6e20c8, 0x4c69105e, 0xd56041e4, 0xa2677172, 0x3c03e4d1, 0x4b04d447, 0xd20d85fd, 0xa50ab56b,
	0x8f659eff, 0x346ed9fc, 0x68ddb3f8, 0x706af48f, 0xe963a535, 0xcb61b38c, 0xcc0c7795, 0xbb0b4703,
	0xa7672661, 0xd06016f7, 0x4969474d, 0x3e6e77db, 0x6b6b51f4, 0x1c6c6162, 0x856530d8, 0xf262004e,
	0x5edef90e, 0xb7bd5c3b, 0xc0ba6cad, 0x6906c2fe, 0x220216b9, 0xec63f226, 0x756aa39c, 0x026d930a,
	0xbdbdf21c, 0xcdd70693, 0x54de5729, 0x23d967bf, 0x72076785, 0x05005713, 0xfed41b76, 0x10da7a5a,
	0x9c0906a9, 0xeb0e363f, 0x17b7be43, 0x60b08ed5, 0x30b5ffe9, 0xd3d6f4fb, 0xc5ba3bbe, 0xb2bd0b28,
	0xead54739, 0x9dd277af, 0xad678846, 0x86d3d2d4, 0xf1d4e242, 0x00000000, 0x77073096, 0xbad03605,
	0xee0e612c, 0x990951ba, 0x53b39330, 0x2cd99e8b, 0xb966d409, 0x67dd4acc, 0xf9b9df6f, 0x8ebeeff9,
	0x5505262f, 0x0bdbdf21, 0x81be16cd, 0xf6b9265b, 0xe3630b12, 0x94643b84, 0x0d6d6a3e, 0xe40ecf0b,
	0x95bf4a82, 0xe2b87a14, 0x7bb12bae, 0x6fb077e1, 0x2bb45a92, 0x5cb36a04, 0xc2d7ffa7, 0xb5d0cf31,
	0x32d86ce3, 0x45df5c75, 0xdcd60dcf, 0xabd13d59, 0x5a05df1b, 0x2d02ef8d, 0x9609a88e, 0xe10e9818,
	0xc8d75180, 0xbfd06116, 0x98d220bc, 0xefd5102a, 0x33031de5, 0xaa0a4c5f, 0xdd0d7cc9, 0x2f6f7c87,
	0x4adfa541, 0x3dd895d7, 0x5bdeae1d, 0xf00f9344, 0x8708a3d2, 0x1e01f268, 0x89d32be0, 0x44042d73,
	0x4db26158, 0x3ab551ce, 0xa3bc0074, 0xd4bb30e2, 0x35b5a8fa, 0x42b2986c, 0xdbbbc9d6, 0xacbcf940,
	0x5768b525, 0x41047a60, 0xdf60efc3, 0xa867df55, 0x04db2615, 0xbe0b1010, 0x18b74777, 0xa4d1c46d,
	0x7cdcefb7, 0x58684c11, 0xc1611dab, 0xb6662d3d, 0x24b4a3a6, 0xa9bcae53, 0xdebb9ec5, 0x47b2cf7f,
	0x0edb8832, 0xe7b82d07, 0x90bf1d91, 0xb0d09822, 0xc7d7a8b4, 0x59b33d17, 0x2eb40d81, 0x7a6a5aa8,
	0x65b0d9c6, 0x12b7e950, 0x8cd37cf3, 0x15da2d49, 0xd9d65adc, 0x40df0b66, 0x37d83bf0, 0x21b4f4b5,
	0xaed16a4a, 0x56b3c423, 0xcfba9599, 0xb8bda50f, 0xedb88320, 0x9abfb3b6, 0x03b6e20c, 0x74b1d29a,
	0xbc66831a, 0x256fd2a0, 0x5268e236, 0x7d079eb1, 0x29d9c998, 0xb3667a2e, 0xc4614ab8, 0x5f058808,
	0xf762575d, 0x806567cb, 0x196c3671, 0x6e6b06e7, 0xfbd44c65, 0x8bbeb8ea, 0xfcb9887c, 0x62dd1ddf,
}

func GetCRC(buf []byte) uint32 {
	if len(buf) < 1 {
		return 0xFFFFFFFF
	}
	crc := uint32(0xFFFFFFFF)
	for _, b := range buf {
		crc = crcTab1[(crc^uint32(b))&0xFF] ^ (crc >> 8)
	}
	return crc ^ 0xFFFFFFFF
}

func GetCRC2(buf []byte) uint32 {
	if len(buf) < 1 {
		return 0xFFFFFFFF
	}
	crc := uint32(0xFFFFFFFF)
	for _, b := range buf {
		crc = crcTab2[(crc^uint32(b))&0xFF] ^ (crc >> 8)
	}
	return crc ^ 0xFFFFFFFF
}

func nextArr(m []byte) []int {
	length := len(m)
	next := make([]int, length)
	next[0] = 0
	for i, j := 1, 0; i < length; i++ {
		for j > 0 && m[j] != m[i] {
			j = next[j-1]
		}
		if m[j] == m[i] {
			j++
		}
		next[i] = j
	}
	return next
}

func KMP(pattern, text []byte) int {
	return bytes.Index(text, pattern)
}

func FindPackHead(buff []byte, startPos int) (pos int, headLen int) {
	length := len(buff)
	if startPos < 0 {
		startPos = 0
	}
	for i := startPos; i <= length-len(packHead); i++ {
		if string(buff[i:i+10]) == string(packHead) {
			return i, 10
		}
	}
	for i := maxInt(0, length-len(packHead)+1); i < length; i++ {
		a := 0
		for j := 0; j < length-i; j++ {
			if buff[i+j] == packHead[j] {
				a++
			} else {
				a = 0
				break
			}
		}
		if a > 0 {
			return i, a
		}
	}
	return -1, 0
}

func FindPackTailPos(buff []byte, startPos int) (pos int, tailLen int) {
	length := len(buff)
	if startPos < 0 {
		startPos = 0
	}
	for i := startPos; i <= length-len(tailPos); i++ {
		if string(buff[i:i+10]) == string(tailPos) {
			return i, 10
		}
	}
	for i := maxInt(0, length-len(tailPos)+1); i < length; i++ {
		a := 0
		for j := 0; j < length-i; j++ {
			if buff[i+j] == tailPos[j] {
				a++
			} else {
				a = 0
				break
			}
		}
		if a > 0 {
			return i, a
		}
	}
	return -1, 0
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func CharToInt(buff []byte, offset int) int {
	if offset+4 > len(buff) {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(buff[offset : offset+4])))
	if err != nil {
		return 0
	}
	return v
}

func Parse(data []byte) (result []byte, ok bool) {
	if len(data) < 30 {
		return nil, false
	}

	head := data[:10]
	tail := data[len(data)-10:]

	if string(head) != string(packHead) {
		return nil, false
	}
	if string(tail) != string(tailPos) {
		return nil, false
	}

	index := 15

	timeEndStr := string(data[index : index+2])
	timeEnd, _ := strconv.Atoi(timeEndStr)
	index += 2

	crcLen1Char := string(data[index : index+1])
	crcLen1, _ := strconv.Atoi(crcLen1Char)
	index++

	headcrc := make([]byte, crcLen1)
	copy(headcrc, data[index:index+crcLen1])
	xycrc1 := make([]byte, 17)
	copy(xycrc1, data[:17])
	c := GetCRC(xycrc1)
	newxycrc1 := []byte(strconv.FormatUint(uint64(c), 16))
	if strings.TrimSpace(string(headcrc)) != strings.TrimSpace(string(newxycrc1)) {
		return nil, false
	}
	index += crcLen1

	messageLen := CharToInt(data, index)
	homeLen := index + 4
	index = index + 4 + messageLen

	xycrc2 := make([]byte, index)
	copy(xycrc2, data[:index])
	c1 := GetCRC2(xycrc2)
	newxycrc2 := []byte(strconv.FormatUint(uint64(c1), 16))

	crcLen2Char := string(data[index : index+1])
	crcLen2, _ := strconv.Atoi(crcLen2Char)
	index++

	allcrc := make([]byte, crcLen2)
	copy(allcrc, data[index:index+crcLen2])
	if strings.TrimSpace(string(allcrc)) != strings.TrimSpace(string(newxycrc2)) {
		return nil, false
	}

	index += 8

	tempChar := string(data[index : index+1])
	timexy, _ := strconv.Atoi(tempChar)

	recvTimeXy := 0
	if timeEnd%2 == 0 {
		recvTimeXy = 1
	}
	if recvTimeXy != timexy {
		return nil, false
	}

	result = make([]byte, messageLen)
	copy(result, data[homeLen:homeLen+messageLen])
	return result, true
}

func ResultBuff(data []byte) []byte {
	head := "["
	tail := "]"
	lenth := []byte(strconv.FormatInt(int64(len(data)), 10))
	for len(lenth) < 8 {
		lenth = append([]byte("0"), lenth...)
	}

	var result []byte
	result = append(result, []byte(head)...)
	result = append(result, lenth...)
	result = append(result, data...)

	if len(data)%2 == 0 {
		result = append(result, '1')
	} else {
		result = append(result, '0')
	}
	result = append(result, []byte(tail)...)
	return result
}

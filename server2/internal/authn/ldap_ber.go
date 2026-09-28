// Минимальный BER-кодек (ITU-T X.690), ровно под три LDAP PDU, которые этому
// пакету нужны — BindRequest/BindResponse, SearchRequest/SearchResultEntry/
// SearchResultDone (RFC 4511 §4.2, §4.5). Не универсальный ASN.1: тегов с
// "high tag number" (>=31) здесь не бывает, значения INTEGER всегда
// неотрицательны и малы (messageID, version, scope, ...) — этого достаточно
// для простого bind + одиночного поиска атрибутов пользователя (см.
// server2/docs/adr/0002-auth-providers.md за обоснование своей реализации
// вместо github.com/go-ldap/ldap).
package authn

import (
	"fmt"
	"io"
)

// Классы/формы тегов, которые встречаются в этом файле.
const (
	berTagInteger        = 0x02
	berTagOctetString    = 0x04
	berTagBoolean        = 0x01
	berTagEnumerated     = 0x0A
	berSeq               = 0x30 // SEQUENCE (universal, constructed)
	berCtxSimpleAuth     = 0x80 // [0] simple (context, primitive) — AuthenticationChoice
	berCtxEqualityFilter = 0xA3 // [3] equalityMatch (context, constructed) — Filter
	berAppBindRequest    = 0x60 // [APPLICATION 0] constructed
	berAppBindResponse   = 0x61 // [APPLICATION 1] constructed
	berAppSearchRequest  = 0x63 // [APPLICATION 3] constructed
	berAppSearchEntry    = 0x64 // [APPLICATION 4] constructed
	berAppSearchDone     = 0x65 // [APPLICATION 5] constructed
)

func berLength(n int) []byte {
	if n < 128 {
		return []byte{byte(n)}
	}
	var b []byte
	for tmp := n; tmp > 0; tmp >>= 8 {
		b = append([]byte{byte(tmp)}, b...)
	}
	return append([]byte{0x80 | byte(len(b))}, b...)
}

func berTLV(tag byte, content []byte) []byte {
	out := make([]byte, 0, 2+len(content))
	out = append(out, tag)
	out = append(out, berLength(len(content))...)
	out = append(out, content...)
	return out
}

func berOctetString(tag byte, s string) []byte { return berTLV(tag, []byte(s)) }

// berPosInt кодирует неотрицательное n в минимальном big-endian
// представлении (с ведущим нулевым байтом, если иначе старший бит
// перепутал бы знак) — этого достаточно для всех INTEGER/ENUMERATED этого
// пакета (messageID, version=3, scope, derefAliases, sizeLimit, timeLimit,
// resultCode).
func berPosInt(tag byte, n int) []byte {
	if n == 0 {
		return berTLV(tag, []byte{0})
	}
	var b []byte
	for v := uint64(n); v > 0; v >>= 8 {
		b = append([]byte{byte(v)}, b...)
	}
	if b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return berTLV(tag, b)
}

func berBool(v bool) []byte {
	if v {
		return berTLV(berTagBoolean, []byte{0xFF})
	}
	return berTLV(berTagBoolean, []byte{0x00})
}

func berPosIntValue(data []byte) int {
	n := 0
	for _, b := range data {
		n = n<<8 | int(b)
	}
	return n
}

// berNode — один разобранный TLV: tag плюс уже вырезанное содержимое
// (вложенные TLV разбираются отдельным вызовом berParse на data).
type berNode struct {
	tag  byte
	data []byte
}

// berParse режет b на последовательность TLV на одном уровне вложенности —
// ровно так LDAP-структуры и разбираются: SEQUENCE.data снова прогоняется
// через berParse для его полей.
func berParse(b []byte) ([]berNode, error) {
	var out []berNode
	i := 0
	for i < len(b) {
		if i+2 > len(b) {
			return nil, io.ErrUnexpectedEOF
		}
		tag := b[i]
		i++
		lenByte := b[i]
		i++
		var length int
		if lenByte&0x80 == 0 {
			length = int(lenByte)
		} else {
			n := int(lenByte & 0x7f)
			if i+n > len(b) {
				return nil, io.ErrUnexpectedEOF
			}
			for k := 0; k < n; k++ {
				length = length<<8 | int(b[i+k])
			}
			i += n
		}
		if length < 0 || i+length > len(b) {
			return nil, fmt.Errorf("authn: ldap: BER length выходит за границы содержимого")
		}
		out = append(out, berNode{tag: tag, data: b[i : i+length]})
		i += length
	}
	return out, nil
}

// readBERMessage читает один top-level TLV из потока (LDAPMessage целиком) —
// достаточно самого внешнего тега/длины, дальше содержимое режет berParse.
func readBERMessage(r io.Reader) (berNode, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return berNode{}, err
	}
	tag := hdr[0]
	var length int
	if hdr[1]&0x80 == 0 {
		length = int(hdr[1])
	} else {
		n := int(hdr[1] & 0x7f)
		if n > 4 {
			return berNode{}, fmt.Errorf("authn: ldap: длина сообщения не помещается в int")
		}
		lb := make([]byte, n)
		if _, err := io.ReadFull(r, lb); err != nil {
			return berNode{}, err
		}
		for _, b := range lb {
			length = length<<8 | int(b)
		}
	}
	content := make([]byte, length)
	if _, err := io.ReadFull(r, content); err != nil {
		return berNode{}, err
	}
	return berNode{tag: tag, data: content}, nil
}

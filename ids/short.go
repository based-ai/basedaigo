// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const ShortIDLen = 20

// ShortEmpty is a useful all zero value
var (
	ShortEmpty = ShortID{}

	_ utils.Sortable[ShortID] = ShortID{}
)

// ShortID wraps a 20 byte hash as an identifier
type ShortID [ShortIDLen]byte

// ShortIDFromRawBytes sets ShortID content to exactly the bytes in src
// This is different from ToShortID, which creates the ShortID of an
// arbitrary-long slice of bytes.
func ShortIDFromRawBytes(src [ShortIDLen]byte) ShortID {
	return src
}

// Writeable is an helper which helps modifiying ShortID content
func WritableShort(id *ShortID) []byte {
	return id[:]
}

// Bytes returns the 20 byte hash as a slice. It is assumed this slice is not
// modified.
func (id ShortID) Bytes() []byte {
	return id[:]
}

// ToShortID attempt to convert a byte slice into an id
func ToShortID(bytes []byte) (ShortID, error) {
	return hashing.ToHash160(bytes)
}

func (id ShortID) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of an ID
	str, _ := cb58.Encode(id.Bytes())
	return str
}

// ShortFromString is the inverse of ShortID.String()
func ShortFromString(idStr string) (ShortID, error) {
	bytes, err := cb58.Decode(idStr)
	if err != nil {
		return ShortID{}, err
	}
	return ToShortID(bytes)
}

func (id ShortID) MarshalJSON() ([]byte, error) {
	str, err := cb58.Encode(id[:])
	if err != nil {
		return nil, err
	}
	return []byte("\"" + str + "\""), nil
}

func (id *ShortID) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	} else if len(str) < 2 {
		return errMissingQuotes
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	// Parse CB58 formatted string to bytes
	bytes, err := cb58.Decode(str[1:lastIndex])
	if err != nil {
		return fmt.Errorf("couldn't decode ID to bytes: %w", err)
	}
	*id, err = ToShortID(bytes)
	return err
}

func (id ShortID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

func (id *ShortID) UnmarshalText(text []byte) error {
	return id.UnmarshalJSON(text)
}

func (id ShortID) Less(other ShortID) bool {
	return bytes.Compare(id[:], other[:]) == -1
}

// ShortFromPrefixedString returns a ShortID assuming the cb58 format is
// prefixed
func ShortFromPrefixedString(idStr, prefix string) (ShortID, error) {
	if !strings.HasPrefix(idStr, prefix) {
		return ShortID{}, fmt.Errorf("ID: %s is missing the prefix: %s", idStr, prefix)
	}
	return ShortFromString(strings.TrimPrefix(idStr, prefix))
}

// Hex returns a hex encoded string of this id.
func (id ShortID) Hex() string {
	return hex.EncodeToString(id.Bytes())
}

// PrefixedString returns the String representation with a prefix added
func (id ShortID) PrefixedString(prefix string) string {
	return prefix + id.String()
}

// ShortIDsToStrings converts an array of shortIDs to an array of their string
// representations
func ShortIDsToStrings(ids []ShortID) []string {
	idStrs := make([]string, len(ids))
	for i, id := range ids {
		idStrs[i] = id.String()
	}
	return idStrs
}

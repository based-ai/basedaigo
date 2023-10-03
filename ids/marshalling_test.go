// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"testing"

	stdjson "encoding/json"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func TestMarshallUnmarshalInversion(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("NodeID", prop.ForAll(
		func(buf string) string {
			var (
				input  = NodeID{buf: buf}
				output = new(NodeID)
			)

			// json package marshalling
			b, err := stdjson.Marshal(input)
			if err != nil {
				return err.Error()
			}
			if err := stdjson.Unmarshal(b, output); err != nil {
				return err.Error()
			}
			if input != *output {
				return fmt.Sprintf("broken inversion original %s, retrieved %s", input, *output)
			}

			// MarshalJson/UnmarshalJson
			output = new(NodeID)
			b, err = input.MarshalJSON()
			if err != nil {
				return err.Error()
			}
			if err := output.UnmarshalJSON(b); err != nil {
				return err.Error()
			}
			if input != *output {
				return fmt.Sprintf("broken inversion original %s, retrieved %s", input, *output)
			}

			return ""
		},
		gen.AnyString()),
	)

	properties.Property("ShortNodeID", prop.ForAll(
		func(buf []byte) string {
			var (
				input  = ShortNodeID(buf)
				output = new(ShortNodeID)
			)

			// json package marshalling
			b, err := stdjson.Marshal(input)
			if err != nil {
				return err.Error()
			}
			if err := stdjson.Unmarshal(b, output); err != nil {
				return err.Error()
			}
			if input != *output {
				return fmt.Sprintf("broken inversion original %s, retrieved %s", input, *output)
			}

			// MarshalJson/UnmarshalJson
			output = new(ShortNodeID)
			b, err = input.MarshalJSON()
			if err != nil {
				return err.Error()
			}
			if err := output.UnmarshalJSON(b); err != nil {
				return err.Error()
			}
			if input != *output {
				return fmt.Sprintf("broken inversion original %s, retrieved %s", input, *output)
			}

			return ""
		},
		gen.SliceOfN(ShortNodeIDLen, gen.UInt8())),
	)

	properties.Property("ShortID", prop.ForAll(
		func(buf []byte) string {
			var (
				input  = ShortID(buf)
				output = new(ShortID)
			)

			// json package marshalling
			b, err := stdjson.Marshal(input)
			if err != nil {
				return err.Error()
			}
			if err := stdjson.Unmarshal(b, output); err != nil {
				return err.Error()
			}
			if input != *output {
				return fmt.Sprintf("broken inversion original %s, retrieved %s", input, *output)
			}

			// MarshalJson/UnmarshalJson
			output = new(ShortID)
			b, err = input.MarshalJSON()
			if err != nil {
				return err.Error()
			}
			if err := output.UnmarshalJSON(b); err != nil {
				return err.Error()
			}
			if input != *output {
				return fmt.Sprintf("broken inversion original %s, retrieved %s", input, *output)
			}

			return ""
		},
		gen.SliceOfN(ShortNodeIDLen, gen.UInt8())),
	)

	properties.Property("ID", prop.ForAll(
		func(buf []byte) string {
			var (
				input  = ID(buf)
				output = new(ID)
			)

			// json package marshalling
			b, err := stdjson.Marshal(input)
			if err != nil {
				return err.Error()
			}
			if err := stdjson.Unmarshal(b, output); err != nil {
				return err.Error()
			}
			if input != *output {
				return fmt.Sprintf("broken inversion original %s, retrieved %s", input, *output)
			}

			// MarshalJson/UnmarshalJson
			output = new(ID)
			b, err = input.MarshalJSON()
			if err != nil {
				return err.Error()
			}
			if err := output.UnmarshalJSON(b); err != nil {
				return err.Error()
			}
			if input != *output {
				return fmt.Sprintf("broken inversion original %s, retrieved %s", input, *output)
			}

			return ""
		},
		gen.SliceOfN(IDLen, gen.UInt8())),
	)

	properties.TestingRun(t)
}
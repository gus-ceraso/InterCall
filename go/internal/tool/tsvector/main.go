package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cerasos/intercall/go/internal/tool/importfixture"
)

type request struct {
	Op    string          `json:"op"`
	Kind  string          `json:"kind"`
	Value json.RawMessage `json:"value"`
	Hex   string          `json:"hex"`
}

type response struct {
	Hex   string `json:"hex,omitempty"`
	Value any    `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

func main() {
	in := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		var req request
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			mustWrite(out, response{Error: err.Error()})
			continue
		}
		if req.Op == "encode" {
			payload, err := encode(req.Kind, req.Value)
			if err != nil {
				mustWrite(out, response{Error: err.Error()})
				continue
			}
			mustWrite(out, response{Hex: hex.EncodeToString(payload)})
			continue
		}
		payload, err := hex.DecodeString(req.Hex)
		if err != nil {
			mustWrite(out, response{Error: err.Error()})
			continue
		}
		value, err := decode(req.Kind, payload)
		if err != nil {
			mustWrite(out, response{Error: err.Error()})
			continue
		}
		mustWrite(out, response{Value: value})
	}
	if err := in.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func encode(kind string, raw json.RawMessage) ([]byte, error) {
	var buf []byte
	switch kind {
	case "uint64":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		var parsed uint64
		if _, err := fmt.Sscan(value, &parsed); err != nil {
			return nil, err
		}
		return importfixture.Codecs.EncodeUint64(buf, parsed)
	case "int64":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		var parsed int64
		if _, err := fmt.Sscan(value, &parsed); err != nil {
			return nil, err
		}
		return importfixture.Codecs.EncodeInt64(buf, parsed)
	case "string":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return importfixture.Codecs.EncodeString(buf, value)
	case "bytes":
		var value []uint8
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return importfixture.Codecs.EncodeBytes(buf, value)
	case "list-uint8":
		var value []uint8
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return importfixture.Codecs.EncodeListUint8(buf, value)
	case "point":
		var value struct{ X, Y float64 }
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return importfixture.Codecs.EncodePoint(buf, structToPoint(value))
	default:
		return nil, fmt.Errorf("unknown vector kind %q", kind)
	}
}

func decode(kind string, payload []byte) (any, error) {
	switch kind {
	case "uint64":
		value, rest, err := importfixture.Codecs.DecodeUint64(payload)
		return checked(value, rest, err, fmt.Sprintf("%d", value))
	case "int64":
		value, rest, err := importfixture.Codecs.DecodeInt64(payload)
		return checked(value, rest, err, fmt.Sprintf("%d", value))
	case "string":
		value, rest, err := importfixture.Codecs.DecodeString(payload)
		return checked(value, rest, err, value)
	case "bytes":
		value, rest, err := importfixture.Codecs.DecodeBytes(payload)
		ints := make([]int, len(value))
		for i, item := range value {
			ints[i] = int(item)
		}
		return checked(value, rest, err, ints)
	case "list-uint8":
		value, rest, err := importfixture.Codecs.DecodeListUint8(payload)
		ints := make([]int, len(value))
		for i, item := range value {
			ints[i] = int(item)
		}
		return checked(value, rest, err, ints)
	case "point":
		value, rest, err := importfixture.Codecs.DecodePoint(payload)
		return checked(value, rest, err, map[string]float64{"x": value.X, "y": value.Y})
	default:
		return nil, fmt.Errorf("unknown vector kind %q", kind)
	}
}

func checked(_ any, rest []byte, err error, value any) (any, error) {
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("trailing bytes")
	}
	return value, nil
}

func structToPoint(value struct{ X, Y float64 }) importfixture.Point {
	return importfixture.Point{X: value.X, Y: value.Y}
}

func mustWrite(out *json.Encoder, value response) {
	if err := out.Encode(value); err != nil {
		panic(err)
	}
}

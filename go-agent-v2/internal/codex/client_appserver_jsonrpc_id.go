package codex

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// jsonRPCID supports both JSON-RPC 2.0 request id shapes: number or string.
type jsonRPCID struct {
	raw json.RawMessage
}

func newJSONRPCIntID(value int64) jsonRPCID {
	return jsonRPCID{raw: json.RawMessage(strconv.FormatInt(value, 10))}
}

func (id jsonRPCID) clone() jsonRPCID {
	if len(id.raw) == 0 {
		return jsonRPCID{}
	}
	raw := append(json.RawMessage(nil), id.raw...)
	return jsonRPCID{raw: raw}
}

func (id jsonRPCID) rawCopy() json.RawMessage {
	if len(id.raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), id.raw...)
}

func (id jsonRPCID) pendingKey() string {
	if value, ok := id.asInt64(); ok {
		return "i:" + strconv.FormatInt(value, 10)
	}
	if value, ok := id.asString(); ok {
		return "s:" + value
	}
	return "raw:" + strings.TrimSpace(string(id.raw))
}

func (id jsonRPCID) logValue() string {
	if value, ok := id.asString(); ok {
		return value
	}
	if value, ok := id.asInt64(); ok {
		return strconv.FormatInt(value, 10)
	}
	return strings.TrimSpace(string(id.raw))
}

func (id jsonRPCID) int64Ptr() *int64 {
	value, ok := id.asInt64()
	if !ok {
		return nil
	}
	v := value
	return &v
}

func (id jsonRPCID) asInt64() (int64, bool) {
	if len(id.raw) == 0 {
		return 0, false
	}
	var value int64
	if err := json.Unmarshal(id.raw, &value); err != nil {
		return 0, false
	}
	return value, true
}

func (id jsonRPCID) asString() (string, bool) {
	if len(id.raw) == 0 {
		return "", false
	}
	var value string
	if err := json.Unmarshal(id.raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func (id jsonRPCID) MarshalJSON() ([]byte, error) {
	if len(id.raw) == 0 {
		return []byte("null"), nil
	}
	return id.raw, nil
}

func (id *jsonRPCID) UnmarshalJSON(data []byte) error {
	if id == nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		id.raw = nil
		return nil
	}

	var intID int64
	if err := json.Unmarshal([]byte(trimmed), &intID); err == nil {
		id.raw = json.RawMessage(strconv.FormatInt(intID, 10))
		return nil
	}

	var stringID string
	if err := json.Unmarshal([]byte(trimmed), &stringID); err == nil {
		raw, marshalErr := json.Marshal(stringID)
		if marshalErr != nil {
			return marshalErr
		}
		id.raw = raw
		return nil
	}

	return fmt.Errorf("jsonrpc id must be string or integer, got: %s", trimmed)
}

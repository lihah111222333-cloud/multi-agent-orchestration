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

func (id jsonRPCID) clone() jsonRPCID { return jsonRPCID{raw: id.rawCopy()} }

func (id jsonRPCID) rawCopy() json.RawMessage {
	if len(id.raw) == 0 { return nil }
	return append(json.RawMessage(nil), id.raw...)
}

func (id jsonRPCID) pendingKey() string {
	if value, ok := id.asInt64(); ok {
		return "i:" + strconv.FormatInt(value, 10)
	}
	if value, ok := id.asString(); ok {
		return "s:" + value
	}
	return "raw:" + id.logValue()
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
	if value, ok := id.asInt64(); ok { return &value }
	return nil
}

func (id jsonRPCID) asInt64() (int64, bool) {
	var value int64
	if len(id.raw) == 0 || json.Unmarshal(id.raw, &value) != nil { return 0, false }
	return value, true
}

func (id jsonRPCID) asString() (string, bool) {
	var value string
	if len(id.raw) == 0 || json.Unmarshal(id.raw, &value) != nil { return "", false }
	return value, true
}

func (id jsonRPCID) MarshalJSON() ([]byte, error) {
	if len(id.raw) == 0 { return []byte("null"), nil }
	return id.raw, nil
}

func (id *jsonRPCID) UnmarshalJSON(data []byte) error {
	if id == nil { return nil }
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" { id.raw = nil; return nil }
	var intID int64
	if err := json.Unmarshal(data, &intID); err == nil {
		id.raw = json.RawMessage(strconv.FormatInt(intID, 10))
		return nil
	}
	var stringID string
	if err := json.Unmarshal(data, &stringID); err == nil {
		id.raw = json.RawMessage(strconv.Quote(stringID))
		return nil
	}
	return fmt.Errorf("jsonrpc id must be string or integer, got: %s", trimmed)
}

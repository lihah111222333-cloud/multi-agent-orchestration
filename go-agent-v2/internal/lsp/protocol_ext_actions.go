package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// SignatureHelpParams textDocument/signatureHelp 请求参数。
type SignatureHelpParams = TextDocumentPositionParams

// SignatureHelpResult 是容错后的稳定签名帮助结构。
type SignatureHelpResult struct {
	Signatures      []SignatureInformationResult `json:"signatures,omitempty"`
	ActiveSignature *int                         `json:"activeSignature,omitempty"`
	ActiveParameter *int                         `json:"activeParameter,omitempty"`
}

// SignatureInformationResult 是单个签名信息。
type SignatureInformationResult struct {
	Label             string                       `json:"label"`
	Documentation     string                       `json:"documentation,omitempty"`
	DocumentationKind string                       `json:"documentationKind,omitempty"`
	Parameters        []ParameterInformationResult `json:"parameters,omitempty"`
}

// ParameterInformationResult 是签名参数信息。
type ParameterInformationResult struct {
	Label             string `json:"label,omitempty"`
	LabelOffsets      []int  `json:"labelOffsets,omitempty"`
	Documentation     string `json:"documentation,omitempty"`
	DocumentationKind string `json:"documentationKind,omitempty"`
}

// DocumentFormattingParams textDocument/formatting 请求参数。
type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

// FormattingOptions 格式化选项。
type FormattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

// decodeSignatureHelp 兼容解码 signatureHelp:
// SignatureHelp | null，其中 documentation 与 label 支持联合类型容错。
func decodeSignatureHelp(raw json.RawMessage) (*SignatureHelpResult, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("decode signatureHelp: %w", err)
	}

	result := &SignatureHelpResult{}
	if signaturesRaw, ok := root["signatures"]; ok {
		var signaturesItems []json.RawMessage
		if err := json.Unmarshal(signaturesRaw, &signaturesItems); err != nil {
			return nil, fmt.Errorf("decode signatureHelp signatures: %w", err)
		}
		signatures := make([]SignatureInformationResult, 0, len(signaturesItems))
		for _, item := range signaturesItems {
			signature, err := decodeSignatureInformation(item)
			if err != nil {
				return nil, err
			}
			signatures = append(signatures, signature)
		}
		result.Signatures = signatures
	}
	if activeSignatureRaw, ok := root["activeSignature"]; ok {
		var value int
		if err := json.Unmarshal(activeSignatureRaw, &value); err == nil {
			result.ActiveSignature = &value
		}
	}
	if activeParameterRaw, ok := root["activeParameter"]; ok {
		var value int
		if err := json.Unmarshal(activeParameterRaw, &value); err == nil {
			result.ActiveParameter = &value
		}
	}

	return result, nil
}

func decodeSignatureInformation(raw json.RawMessage) (SignatureInformationResult, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return SignatureInformationResult{}, fmt.Errorf("decode signature info: %w", err)
	}

	var result SignatureInformationResult
	if labelRaw, ok := obj["label"]; ok {
		if err := json.Unmarshal(labelRaw, &result.Label); err != nil {
			result.Label = string(bytes.TrimSpace(labelRaw))
		}
	}
	if documentationRaw, ok := obj["documentation"]; ok {
		result.Documentation, result.DocumentationKind = decodeStringOrMarkup(documentationRaw)
	}
	if parametersRaw, ok := obj["parameters"]; ok {
		var parameterItems []json.RawMessage
		if err := json.Unmarshal(parametersRaw, &parameterItems); err != nil {
			return SignatureInformationResult{}, fmt.Errorf("decode signature parameters: %w", err)
		}

		parameters := make([]ParameterInformationResult, 0, len(parameterItems))
		for _, item := range parameterItems {
			parameter, err := decodeParameterInformation(item)
			if err != nil {
				return SignatureInformationResult{}, err
			}
			parameters = append(parameters, parameter)
		}
		result.Parameters = parameters
	}
	return result, nil
}

func decodeParameterInformation(raw json.RawMessage) (ParameterInformationResult, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ParameterInformationResult{}, fmt.Errorf("decode signature parameter: %w", err)
	}

	var result ParameterInformationResult
	if labelRaw, ok := obj["label"]; ok {
		result.Label, result.LabelOffsets = decodeParameterLabel(labelRaw)
	}
	if documentationRaw, ok := obj["documentation"]; ok {
		result.Documentation, result.DocumentationKind = decodeStringOrMarkup(documentationRaw)
	}

	return result, nil
}

func decodeStringOrMarkup(raw json.RawMessage) (string, string) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, "plaintext"
	}

	var markup struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &markup); err == nil {
		return markup.Value, markup.Kind
	}

	return "", ""
}

func decodeParameterLabel(raw json.RawMessage) (string, []int) {
	var label string
	if err := json.Unmarshal(raw, &label); err == nil {
		return label, nil
	}

	var offsets []int
	if err := json.Unmarshal(raw, &offsets); err == nil && len(offsets) == 2 {
		return "", offsets
	}

	return string(bytes.TrimSpace(raw)), nil
}

// decodeTextEdits 兼容解码 formatting 返回:
// []TextEdit | null
func decodeTextEdits(raw json.RawMessage) ([]TextEdit, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var edits []TextEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		return nil, fmt.Errorf("decode text edits: %w", err)
	}
	return edits, nil
}

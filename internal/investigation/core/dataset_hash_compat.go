package core

import (
	"bytes"
	"encoding/json"
	"errors"

	"pocketcli/internal/investigation/storage"
)

// publishedDatasetHash verifies the bytes persisted at publication time. A
// current Go struct may contain fields that did not exist when an older
// snapshot was published, so unmarshalling and re-marshalling changes its hash.
func publishedDatasetHash(raw []byte) (string, error) {
	raw = bytes.TrimSpace(raw)
	if !json.Valid(raw) {
		return "", errors.New("dataset JSON inválido")
	}
	var outer struct {
		Snapshot json.RawMessage `json:"Snapshot"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil || len(outer.Snapshot) == 0 {
		return "", errors.New("snapshot ausente no dataset")
	}
	prefix := []byte(`{"Snapshot":`)
	if !bytes.HasPrefix(raw, prefix) || !bytes.HasPrefix(raw[len(prefix):], outer.Snapshot) {
		return "", errors.New("formato publicado do dataset inválido")
	}
	position := len(prefix)
	snapshot := append([]byte(nil), outer.Snapshot...)
	decoder := json.NewDecoder(bytes.NewReader(snapshot))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return "", errors.New("snapshot JSON inválido")
	}
	targets := map[string]bool{"id": true, "dataset_hash": true, "published_at": true}
	type span struct{ start, end int }
	spans := make([]span, 0, len(targets))
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return "", err
		}
		key, ok := keyToken.(string)
		if !ok {
			return "", errors.New("chave inválida no snapshot")
		}
		start := int(decoder.InputOffset())
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return "", err
		}
		if !targets[key] {
			continue
		}
		segment := snapshot[start:int(decoder.InputOffset())]
		colon := bytes.IndexByte(segment, ':')
		if colon < 0 {
			return "", errors.New("valor inválido no snapshot")
		}
		valueStart := start + colon + 1
		for valueStart < len(snapshot) && (snapshot[valueStart] == ' ' || snapshot[valueStart] == '\t') {
			valueStart++
		}
		if !bytes.Equal(snapshot[valueStart:int(decoder.InputOffset())], value) || len(value) < 2 || value[0] != '"' {
			return "", errors.New("campo de publicação inválido")
		}
		spans = append(spans, span{valueStart, int(decoder.InputOffset())})
		delete(targets, key)
	}
	if len(targets) != 0 {
		return "", errors.New("campos de publicação ausentes")
	}
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		snapshot = append(append(append([]byte(nil), snapshot[:s.start]...), '"', '"'), snapshot[s.end:]...)
	}
	canonical := append(append(append([]byte(nil), raw[:position]...), snapshot...), raw[position+len(outer.Snapshot):]...)
	return storage.Hash(canonical), nil
}

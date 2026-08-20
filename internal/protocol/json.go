package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

var identityPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,160}$`)

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

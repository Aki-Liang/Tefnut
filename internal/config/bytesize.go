package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ByteSize is an int64 byte count that unmarshals from either a raw number
// (2147483648) or a binary-unit size string ("2GiB", "512MiB", "0") — the
// same forms the TEFNUT_* env overrides accept.
type ByteSize int64

func (b *ByteSize) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("config: size must be a number or string like 2GiB, got %s", value.Tag)
	}
	n, err := parseSize(value.Value)
	if err != nil {
		return err
	}
	*b = ByteSize(n)
	return nil
}

package seed

import "fmt"

type TargetType string

const (
	TargetTypeImage   TargetType = "image"
	TargetTypeHFModel TargetType = "hf-model"
)

type Target struct {
	Type       TargetType
	Image      string
	Model      string
	HFCacheDir string
}

func ParseTargetType(raw string) (TargetType, error) {
	switch TargetType(raw) {
	case TargetTypeImage, TargetTypeHFModel:
		return TargetType(raw), nil
	default:
		return "", fmt.Errorf("invalid seed-source %q (must be one of: image, hf-model)", raw)
	}
}

func (t Target) Validate() error {
	switch t.Type {
	case TargetTypeImage:
		if t.Image == "" {
			return fmt.Errorf("image seeding requires --image")
		}
	case TargetTypeHFModel:
		if t.Model == "" {
			return fmt.Errorf("hf-model seeding requires --hf-model")
		}
		if t.HFCacheDir == "" {
			return fmt.Errorf("hf-model seeding requires --hf-cache-dir")
		}
	default:
		return fmt.Errorf("unsupported seed target type %q", t.Type)
	}
	return nil
}

func (t Target) Identifier() string {
	if t.Type == TargetTypeHFModel {
		return t.Model
	}
	return t.Image
}

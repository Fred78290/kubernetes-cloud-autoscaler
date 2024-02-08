package constantes

import (
	"encoding/json"
	"fmt"
)

// Copy Make a deep copy from src into dst.
func Copy(dst any, src any) error {
	if dst == nil {
		return fmt.Errorf("dst cannot be nil")
	}

	if src == nil {
		return fmt.Errorf("src cannot be nil")
	}

	bytes, err := json.Marshal(src)

	if err != nil {
		return fmt.Errorf("unable to marshal src: %s", err)
	}

	err = json.Unmarshal(bytes, dst)

	if err != nil {
		return fmt.Errorf("unable to unmarshal into dst: %s", err)
	}

	return nil
}

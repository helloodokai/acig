package reporters

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/helloodokai/acig/internal/verdict"
)

func WriteJSON(v *verdict.Verdict, path string) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling verdict: %w", err)
	}
	if path == "" || path == "-" {
		fmt.Println(string(b))
		return nil
	}
	return os.WriteFile(path, b, 0o644)
}

func WriteJSONStdout(v *verdict.Verdict) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling verdict: %w", err)
	}
	fmt.Println(string(b))
	return nil
}
package verdict

import (
	"encoding/json"
	"os"

	"github.com/invopop/jsonschema"
)

func Schema() ([]byte, error) {
	s := jsonschema.Reflect(&Verdict{})
	s.ID = "https://github.com/helloodokai/acig/schema/verdict.schema.json"
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return b, nil
}

func WriteSchemaFile(path string) error {
	b, err := Schema()
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
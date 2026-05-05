package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/helloodokai/acig/internal/verdict"
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Print the Verdict JSON schema",
	RunE:  runSchema,
}

func init() {
	rootCmd.AddCommand(schemaCmd)
}

func runSchema(cmd *cobra.Command, args []string) error {
	b, err := verdict.Schema()
	if err != nil {
		return fmt.Errorf("generating schema: %w", err)
	}
	fmt.Println(string(b))
	return nil
}
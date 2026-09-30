package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/entwico/gqlgoclientgen/pkg/schema"
)

// introspectFlags holds the flag values for the introspect command.
type introspectFlags struct {
	url        string
	headers    []string
	configFile string
	format     string
	output     string
}

var intFlags introspectFlags

// IntrospectConfig describes the JSON config file accepted by the introspect
// command via --config.
type IntrospectConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

var introspectCmd = &cobra.Command{
	Use:   "introspect [URL]",
	Short: "Introspect a GraphQL endpoint and retrieve its schema",
	Long: `Introspect fetches the GraphQL schema from a specified endpoint using the
introspection query. The schema can be output as JSON or SDL format.

You can provide the URL either as a command argument or through a config file.
Command-line arguments take precedence over config file values.`,
	Args:         cobra.MaximumNArgs(1),
	RunE:         runIntrospect,
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(introspectCmd)

	introspectCmd.Flags().StringVarP(&intFlags.url, "url", "u", "", "GraphQL endpoint URL")
	introspectCmd.Flags().StringArrayVarP(&intFlags.headers, "header", "H", nil, "HTTP headers to include in the request (format: 'Key: Value')")
	introspectCmd.Flags().StringVarP(&intFlags.configFile, "config", "c", "", "Config file path (JSON format with url and headers)")
	introspectCmd.Flags().StringVarP(&intFlags.format, "format", "f", "json", "Output format: json or sdl")
	introspectCmd.Flags().StringVarP(&intFlags.output, "output", "o", "", "Output file path (default: stdout)")
}

func runIntrospect(cmd *cobra.Command, args []string) error {
	// work on a copy so runs never leak state into the package-level flags
	flags := intFlags

	// validate the format before doing any network work
	format := strings.ToLower(flags.format)
	if format != "json" && format != "sdl" {
		return fmt.Errorf("invalid format: %s (must be 'json' or 'sdl')", flags.format)
	}

	var (
		endpointURL string
		allHeaders  = make(map[string]string)
	)

	if flags.configFile != "" {
		config, err := loadIntrospectConfig(flags.configFile)
		if err != nil {
			return fmt.Errorf("failed to load config file: %w", err)
		}

		endpointURL = config.URL

		maps.Copy(allHeaders, config.Headers)
	}

	// priority order: positional arg > --url flag > config file
	switch {
	case len(args) > 0:
		endpointURL = args[0]
	case flags.url != "":
		endpointURL = flags.url
	case endpointURL == "":
		return errors.New("URL is required (provide as argument, --url flag, or in config file)")
	default:
		// keep the URL from the config file
	}

	// command-line headers override config file headers
	if err := parseHeaderFlags(flags.headers, allHeaders); err != nil {
		return err
	}

	schemaData, err := schema.FetchSchema(cmd.Context(), endpointURL, newHTTPClient(allHeaders))
	if err != nil {
		return fmt.Errorf("failed to fetch schema: %w", err)
	}

	var outputData []byte

	if format == "sdl" {
		sdl, err := schema.ToSDL(schemaData)
		if err != nil {
			return fmt.Errorf("failed to convert schema to SDL: %w", err)
		}

		outputData = []byte(sdl)
	} else {
		jsonData, err := json.MarshalIndent(schemaData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal schema: %w", err)
		}

		outputData = jsonData
	}

	if flags.output != "" {
		// schema files are meant to be committed and world-readable
		err := os.WriteFile(flags.output, outputData, 0o644) //nolint:gosec
		if err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}

		fmt.Fprintf(os.Stderr, "schema written to %s\n", flags.output)

		return nil
	}

	fmt.Println(string(outputData))

	return nil
}

// loadIntrospectConfig loads the configuration from a JSON file.
func loadIntrospectConfig(path string) (*IntrospectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config IntrospectConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

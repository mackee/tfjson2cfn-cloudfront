// Command tfjson2cfn-cloudfront converts a Terraform plan (terraform show
// -json) into a CloudFront-only CloudFormation template for localfront.
//
//	terraform show -json plan.tfplan | tfjson2cfn-cloudfront > template.yaml
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/alecthomas/kong"
	"github.com/mackee/tfjson2cfn-cloudfront/internal/converter"
)

type cli struct {
	Input    string `kong:"name='input',short='i',default='-',help='Path to terraform show -json output (- for stdin).'"`
	Output   string `kong:"name='output',short='o',default='-',help='Path to write the CloudFormation template (- for stdout).'"`
	Format   string `kong:"name='format',default='yaml',enum='yaml,json',help='Output format: yaml or json.'"`
	LogLevel string `kong:"name='log-level',default='info',enum='debug,info,warn,error',help='Log verbosity (to stderr).'"`
}

func main() {
	var c cli
	kctx := kong.Parse(&c,
		kong.Name("tfjson2cfn-cloudfront"),
		kong.Description("Convert a Terraform plan (terraform show -json) into a CloudFront-only CloudFormation template for localfront."),
		kong.UsageOnError(),
	)
	if err := run(c); err != nil {
		kctx.FatalIfErrorf(err)
	}
}

func run(c cli) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLevel(c.LogLevel)}))

	data, err := readInput(c.Input)
	if err != nil {
		return err
	}

	res, err := converter.Convert(data, converter.Options{Format: c.Format})
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		logger.Warn(w)
	}

	return writeOutput(c.Output, res.Output)
}

func readInput(path string) ([]byte, error) {
	if path == "" || path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func writeOutput(path string, data []byte) error {
	if path == "" || path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

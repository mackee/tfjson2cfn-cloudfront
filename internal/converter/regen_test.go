package converter

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// update regenerates the example plan.json fixtures (the Go replacement for the
// old examples/regenerate.sh). It runs terraform, so it needs terraform on PATH
// (see aqua.yaml) and network access for the AWS provider download.
var update = flag.Bool("update", false,
	"regenerate examples/<name>/plan.json from their Terraform (runs terraform; needs network)")

// TestMain regenerates the fixtures before the suite runs when -update is given:
//
//	go test ./internal/converter -update
//
// Without -update nothing runs terraform, so the suite needs neither terraform
// nor network — it converts the committed plan.json fixtures and compares.
func TestMain(m *testing.M) {
	flag.Parse()
	if *update {
		if err := regenerateFixtures(); err != nil {
			fmt.Fprintln(os.Stderr, "regenerate fixtures:", err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// regenerateFixtures runs terraform for every example with a main.tf and writes
// a fresh plan.json. A shared plugin cache avoids re-downloading the provider
// per example.
func regenerateFixtures() error {
	entries, err := os.ReadDir(examplesRoot)
	if err != nil {
		return err
	}
	cache, err := filepath.Abs(filepath.Join(examplesRoot, "..", ".tf-plugin-cache"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return err
	}
	env := append(os.Environ(), "TF_IN_AUTOMATION=1", "TF_PLUGIN_CACHE_DIR="+cache)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(examplesRoot, e.Name())
		if !fileExists(filepath.Join(dir, "main.tf")) {
			continue
		}
		fmt.Fprintf(os.Stderr, "==> regenerating %s/plan.json\n", e.Name())
		if err := regeneratePlan(dir, env); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
	}
	return nil
}

func regeneratePlan(dir string, env []string) error {
	const planFile = ".regen.tfplan"
	defer func() { _ = os.Remove(filepath.Join(dir, planFile)) }()

	run := func(args ...string) error {
		cmd := exec.Command("terraform", args...)
		cmd.Env = env
		// Keep terraform's chatter on stderr so stdout stays clean.
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if err := run("-chdir="+dir, "init", "-input=false", "-no-color"); err != nil {
		return fmt.Errorf("terraform init: %w", err)
	}
	if err := run("-chdir="+dir, "plan", "-input=false", "-no-color", "-out="+planFile); err != nil {
		return fmt.Errorf("terraform plan: %w", err)
	}

	show := exec.Command("terraform", "-chdir="+dir, "show", "-json", planFile)
	show.Env = env
	var out bytes.Buffer
	show.Stdout = &out
	show.Stderr = os.Stderr
	if err := show.Run(); err != nil {
		return fmt.Errorf("terraform show -json: %w", err)
	}

	normalized, err := normalizePlan(out.Bytes())
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "plan.json"), normalized, 0o644)
}

// normalizePlan drops the volatile top-level "timestamp" and re-encodes the
// plan deterministically (sorted keys, indented) so a regenerated fixture only
// changes when the Terraform actually did.
func normalizePlan(data []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode plan json: %w", err)
	}
	delete(doc, "timestamp")
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

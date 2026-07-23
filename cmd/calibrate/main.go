// Command calibrate measures how reliably an LLM provider follows a suite of
// scenarios whose correct answers are known — a deterministic reliability canary
// (see internal/calibration). It runs standalone, independent of the Talunor
// agent, so the *same* YAML suite can be pointed at any provider to compare them
// on one yardstick, and re-run over time to catch silent quality drift.
//
// Usage:
//
//	calibrate --suite docs/calibration.seed.yaml
//	calibrate --suite my.yaml --save-baseline base.json
//	calibrate --suite my.yaml --baseline base.json   # exit 1 on regression
//	calibrate encrypt --in my.yaml --out my.enc      # needs CALIBRATION_KEY
//
// The provider is selected from the environment (TALUNOR_PROVIDER / TALUNOR_MODEL
// and the provider URL/key vars, same as the agent); a .env is loaded first. A
// suite file may be an encrypted envelope; set CALIBRATION_KEY to decrypt it (an
// unset key on a plaintext suite is fine — see internal/calibration for the
// threat model).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/lao-tseu-is-alive/Talunor/internal/calibration"
	"github.com/lao-tseu-is-alive/Talunor/internal/config"
	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "encrypt" {
		os.Exit(encryptCmd(os.Args[2:]))
	}
	os.Exit(run())
}

// encryptCmd encrypts a plaintext suite into a versionable envelope using
// CALIBRATION_KEY. It validates the input parses as a suite before encrypting, so
// a garbled file fails fast rather than becoming an undecipherable blob.
func encryptCmd(args []string) int {
	fs := flag.NewFlagSet("encrypt", flag.ContinueOnError)
	in := fs.String("in", "", "plaintext YAML suite to encrypt (required)")
	out := fs.String("out", "", "output encrypted file (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "calibrate encrypt: --in and --out are required")
		return 2
	}
	if err := config.LoadDotEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "calibrate encrypt: load .env: %v\n", err)
		return 2
	}
	key := os.Getenv("CALIBRATION_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "calibrate encrypt: CALIBRATION_KEY must be set")
		return 2
	}
	plain, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "calibrate encrypt: %v\n", err)
		return 2
	}
	suite, err := calibration.Parse(plain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "calibrate encrypt: %v\n", err)
		return 2
	}
	enc, err := calibration.EncryptSuite(plain, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "calibrate encrypt: %v\n", err)
		return 2
	}
	if err := os.WriteFile(*out, enc, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "calibrate encrypt: %v\n", err)
		return 2
	}
	fmt.Fprintf(os.Stderr, "calibrate: encrypted %s → %s (%d scenarios)\n", *in, *out, len(suite.Scenarios))
	return 0
}

// run returns the process exit code: 0 ok, 1 a regression against --baseline, 2 a
// setup error (bad flags, unreadable suite, provider selection).
func run() int {
	var (
		suitePath    = flag.String("suite", "", "path to a YAML scenario suite (required)")
		runs         = flag.Int("runs", 0, "default repeats per scenario (0 = 1; a scenario's own `runs` wins)")
		temperature  = flag.Float64("temperature", 0, "default sampling temperature (0 = provider default)")
		baselinePath = flag.String("baseline", "", "compare against this baseline JSON; exit 1 on regression")
		saveBaseline = flag.String("save-baseline", "", "write this run as a baseline JSON")
		threshold    = flag.Float64("threshold", 0.05, "regression threshold (pass-rate drop) for --baseline")
		asJSON       = flag.Bool("json", false, "emit the report as JSON instead of text")
	)
	flag.Parse()

	if *suitePath == "" {
		fmt.Fprintln(os.Stderr, "calibrate: --suite is required")
		flag.Usage()
		return 2
	}

	// Load .env first (real env wins), then select the provider from the env.
	if err := config.LoadDotEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "calibrate: load .env: %v\n", err)
		return 2
	}
	provider, model, err := llm.FromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "calibrate: provider: %v\n", err)
		return 2
	}

	// Source-agnostic load: decrypts first if the file is an encrypted envelope and
	// CALIBRATION_KEY is set; a plaintext suite passes straight through.
	suite, err := calibration.LoadMaybeEncrypted(*suitePath, os.Getenv("CALIBRATION_KEY"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "calibrate: %v\n", err)
		return 2
	}

	rep := calibration.Run(context.Background(), provider, suite, calibration.Options{
		DefaultRuns: *runs,
		Temperature: *temperature,
		Model:       model,
	})

	if *asJSON {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "calibrate: marshal report: %v\n", err)
			return 2
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(rep.String())
	}

	if *saveBaseline != "" {
		if err := rep.AsBaseline().Save(*saveBaseline); err != nil {
			fmt.Fprintf(os.Stderr, "calibrate: save baseline: %v\n", err)
			return 2
		}
		fmt.Fprintf(os.Stderr, "calibrate: baseline written to %s\n", *saveBaseline)
	}

	if *baselinePath != "" {
		base, err := calibration.LoadBaseline(*baselinePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "calibrate: %v\n", err)
			return 2
		}
		drift := rep.Diff(base, *threshold)
		if drift.Regressed() {
			fmt.Fprintf(os.Stderr, "calibrate: REGRESSION vs baseline (overall Δ %+.2f):\n", drift.OverallDelta)
			for _, r := range drift.Regressions {
				fmt.Fprintf(os.Stderr, "  %s: %.0f%% → %.0f%% (Δ %.0f pts)\n",
					r.Scope, r.Baseline*100, r.Current*100, r.Delta*100)
			}
			return 1
		}
		fmt.Fprintf(os.Stderr, "calibrate: no regression vs baseline (overall Δ %+.2f)\n", drift.OverallDelta)
	}

	return 0
}

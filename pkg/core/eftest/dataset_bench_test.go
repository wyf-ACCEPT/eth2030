package eftest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// datasetExists returns true if the JSON file or its .zip archive exists.
func datasetExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	if _, err := os.Stat(path + ".zip"); err == nil {
		return true
	}
	return false
}

func TestDatasetBenchmark(t *testing.T) {
	// Look for the dataset file (or .zip) in the data/ directory.
	datasetPath := filepath.Join("..", "..", "..", "data", "uniswap-pf-t1k-c0.json")
	if !datasetExists(datasetPath) {
		t.Skipf("dataset not found at %s (or .zip)", datasetPath)
	}

	result, err := LoadAndRunDataset(datasetPath)
	if err != nil {
		t.Fatalf("LoadAndRunDataset: %v", err)
	}

	// Print results.
	t.Log("")
	t.Log(strings.Repeat("=", 60))
	t.Log("  UNISWAP DATASET BENCHMARK RESULTS")
	t.Log(strings.Repeat("=", 60))
	t.Logf("  Transactions:    %d total, %d success, %d failed", result.TxCount, result.TxSuccess, result.TxFailed)
	t.Logf("  Total gas used:  %d", result.TotalGasUsed)
	t.Logf("  Avg gas/tx:      %.0f", result.GasPerTx)
	t.Logf("  Duration:        %v", result.Duration)
	t.Log(strings.Repeat("-", 60))
	t.Logf("  TPS:             %.2f tx/s", result.TPS)
	t.Logf("  Gas throughput:  %.2f Mgas/s", result.MGasPerSec)
	t.Logf("  Gas throughput:  %.4f Ggas/s", result.MGasPerSec/1000)
	t.Log(strings.Repeat("=", 60))

	if len(result.Errors) > 0 {
		t.Log("")
		t.Logf("First %d errors:", len(result.Errors))
		for _, e := range result.Errors {
			t.Logf("  %s", e)
		}
	}

	if result.TxSuccess == 0 {
		t.Error("expected at least one successful transaction")
	}
}

func BenchmarkDatasetExecution(b *testing.B) {
	datasetPath := filepath.Join("..", "..", "..", "data", "uniswap-pf-t1k-c0.json")
	if !datasetExists(datasetPath) {
		b.Skipf("dataset not found at %s (or .zip)", datasetPath)
	}

	for i := 0; i < b.N; i++ {
		result, err := LoadAndRunDataset(datasetPath)
		if err != nil {
			b.Fatalf("LoadAndRunDataset: %v", err)
		}
		b.ReportMetric(result.TPS, "tx/s")
		b.ReportMetric(result.MGasPerSec, "Mgas/s")
		b.ReportMetric(float64(result.TxSuccess), "success")
	}
}

// TestDatasetBenchmarkCustomPath allows running with a custom dataset path.
// Usage: DATASET_PATH=/path/to/file.json go test -run TestDatasetBenchmarkCustomPath ./core/eftest/
var datasetFlag string

func init() {
	datasetFlag = os.Getenv("DATASET_PATH")
}

func TestDatasetBenchmarkCustomPath(t *testing.T) {
	if datasetFlag == "" {
		t.Skip("set DATASET_PATH env var to run with custom dataset")
	}

	if !datasetExists(datasetFlag) {
		t.Fatalf("dataset not found: %s (or .zip)", datasetFlag)
	}

	result, err := LoadAndRunDataset(datasetFlag)
	if err != nil {
		t.Fatalf("LoadAndRunDataset: %v", err)
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("  DATASET BENCHMARK RESULTS")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  File:            %s\n", datasetFlag)
	fmt.Printf("  Transactions:    %d total, %d success, %d failed\n", result.TxCount, result.TxSuccess, result.TxFailed)
	fmt.Printf("  Total gas used:  %d\n", result.TotalGasUsed)
	fmt.Printf("  Avg gas/tx:      %.0f\n", result.GasPerTx)
	fmt.Printf("  Duration:        %v\n", result.Duration)
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("  TPS:             %.2f tx/s\n", result.TPS)
	fmt.Printf("  Gas throughput:  %.2f Mgas/s\n", result.MGasPerSec)
	fmt.Printf("  Gas throughput:  %.4f Ggas/s\n", result.MGasPerSec/1000)
	fmt.Println(strings.Repeat("=", 60))
}

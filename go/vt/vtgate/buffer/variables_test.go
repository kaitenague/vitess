/*
Copyright 2019 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package buffer

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"vitess.io/vitess/go/stats"
)

func TestVariables(t *testing.T) {
	fs := pflag.NewFlagSet("vtgate_buffer_variables_test", pflag.ContinueOnError)
	registerFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Errorf("failed to parse with default values: %v", err)
	}

	fs.Set("buffer_size", "23")
	defer func() {
		fs.Set("buffer_size", "1")
	}()

	// Create new buffer which will the flags.
	NewConfigFromFlags()

	if got, want := bufferSizeStat.Get(), int64(23); got != want {
		t.Fatalf("BufferSize variable not set during initilization: got = %v, want = %v", got, want)
	}
}

func TestVariablesAreInitialized(t *testing.T) {
	// Create a new buffer and make a call which will create the shardBuffer object.
	// After that, the variables should be initialized for that shard.
	b := New(NewDefaultConfig())
	_, err := b.WaitForFailoverEnd(context.Background(), "init_test", "0", nil, nil)
	if err != nil {
		t.Fatalf("buffer should just passthrough and not return an error: %v", err)
	}

	statsKey := []string{"init_test", "0"}
	type testCase struct {
		desc     string
		counter  *stats.CountersWithMultiLabels
		statsKey []string
	}
	testCases := []testCase{
		{"starts", starts, statsKey},
		{"failoverDurationSumMs", failoverDurationSumMs, statsKey},
		{"utilizationSum", &utilizationSum.CountersWithMultiLabels, statsKey},
		{"utilizationDryRunSum", utilizationDryRunSum, statsKey},
		{"requestsBuffered", requestsBuffered, statsKey},
		{"requestsBufferedDryRun", requestsBufferedDryRun, statsKey},
		{"requestsDrained", requestsDrained, statsKey},
	}
	for _, r := range stopReasons {
		testCases = append(testCases, testCase{"stops", stops, append(statsKey, string(r))})
	}
	for _, r := range evictReasons {
		testCases = append(testCases, testCase{"evicted", requestsEvicted, append(statsKey, string(r))})
	}
	for _, r := range skippedReasons {
		testCases = append(testCases, testCase{"skipped", requestsSkipped, append(statsKey, string(r))})
	}

	for _, tc := range testCases {
		wantValue := 0
		if len(tc.statsKey) == 3 && tc.statsKey[2] == string(skippedDisabled) {
			// The request passed through above was registered as skipped.
			wantValue = 1
		}
		if err := checkEntry(tc.counter, tc.statsKey, wantValue); err != nil {
			t.Fatalf("variable: %v not correctly initialized: %v", tc.desc, err)
		}
	}
}

func checkEntry(counters *stats.CountersWithMultiLabels, statsKey []string, want int) error {
	name := strings.Join(statsKey, ".")
	got, ok := counters.Counts()[name]
	if !ok {
		return fmt.Errorf("no entry for: %v", name)
	}
	if got != int64(want) {
		return fmt.Errorf("wrong value for entry: %v got = %v, want = %v", name, got, want)
	}

	return nil
}

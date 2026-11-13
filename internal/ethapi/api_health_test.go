package ethapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rpc"
)

type healthTestAPI struct{}

func (healthTestAPI) Ping() bool          { return true }
func (healthTestAPI) Fail() (bool, error) { return false, errors.New("test failure") }

func TestRPCHealth(t *testing.T) {
	// Metrics can only be enabled at startup. Isolate each configuration from
	// other tests and run both through the actual HTTP RPC handler.
	mode := os.Getenv("BSC_RPC_HEALTH_TEST")
	if mode == "" {
		for _, mode := range []string{"disabled", "enabled"} {
			t.Run(mode, func(t *testing.T) {
				cmd := exec.Command(os.Args[0], "-test.run=^TestRPCHealth$", "-test.v")
				cmd.Env = append(os.Environ(), "BSC_RPC_HEALTH_TEST="+mode)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%v\n%s", err, out)
				}
			})
		}
		return
	}
	if mode == "enabled" {
		metrics.Enable()
	}
	server := rpc.NewServer()
	defer server.Stop()
	if err := server.RegisterName("eth", NewBlockChainAPI(nil)); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterName("test", healthTestAPI{}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	client, err := rpc.DialHTTP(httpServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	check := func(want bool) {
		t.Helper()
		var healthy bool
		if err := client.CallContext(ctx, &healthy, "eth_health"); err != nil {
			t.Fatal(err)
		}
		if healthy != want {
			t.Fatalf("eth_health = %v, want %v", healthy, want)
		}
	}
	check(true) // Preserve the existing behavior before any business calls.

	// Inject completed slow calls without making the test wait seven seconds.
	for range 8 {
		rpc.RpcServingHealthSample.Update(int64(7 * time.Second))
	}
	for range 25 {
		check(false)
	}
	if got := rpc.RpcServingHealthSample.Snapshot().Count(); got != 8 {
		t.Fatalf("health probes changed the sample count to %d, want 8", got)
	}

	// Fast business requests, rather than probes, establish recovery.
	for range 25 {
		var result bool
		if err := client.CallContext(ctx, &result, "test_ping"); err != nil {
			t.Fatal(err)
		}
	}
	check(true)
	if got := rpc.RpcServingHealthSample.Snapshot().Count(); got != 33 {
		t.Fatalf("business calls: sample count = %d, want 33", got)
	}
	if err := client.CallContext(ctx, nil, "test_fail"); err == nil {
		t.Fatal("expected failed business call")
	}
	if got := rpc.RpcServingHealthSample.Snapshot().Count(); got != 34 {
		t.Fatalf("failed call was not sampled: count = %d, want 34", got)
	}
}

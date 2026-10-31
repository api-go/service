package service_test

import (
	"apigo.cc/go/discover"
	"apigo.cc/go/redis"
	"apigo.cc/go/service"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"
)

func TestServiceDiscoveryDoesNotBlockStartup(t *testing.T) {
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	discover.Init()
	original := discover.Config
	defer func() { discover.Stop(); discover.Config = original }()
	app := fmt.Sprintf("go-discover-service-recovery-%d", time.Now().UnixNano())
	registry := fmt.Sprintf("redis://127.0.0.1:%d/0", port)
	ws := service.NewWebServer()
	ws.Config.Listen = "127.0.0.1:0,http"
	ws.Config.App = app
	ws.Config.Register = registry
	before := time.Now()
	if err := ws.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	defer ws.Stop(context.Background())
	if time.Since(before) > time.Second {
		t.Fatal("Service startup waited for Redis")
	}
	checkHealth := func() int {
		response := httptest.NewRecorder()
		ws.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, discover.HealthPath, nil))
		return response.Code
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + ws.Addr + discover.HealthPath)
	if err != nil {
		t.Fatal("HTTP listener was not available during Redis outage", err)
	}
	response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal("network health did not report unready", response.StatusCode)
	}
	if checkHealth() != 503 {
		t.Fatal("unregistered service is ready")
	}
	process := exec.Command(binary, "--bind", "127.0.0.1", "--port", fmt.Sprint(port), "--save", "", "--appendonly", "no")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { process.Process.Kill(); process.Wait() }()
	timeout := time.NewTimer(8 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for !discover.IsRegistered() {
		select {
		case <-timeout.C:
			t.Fatal("Service registration did not recover")
		case <-tick.C:
		}
	}
	if checkHealth() != 200 {
		t.Fatal("recovered service is not ready")
	}
	// First-start registration failed, but Service still owns and stops the workers.
	if err := ws.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if discover.IsRegistered() || discover.IsServer() {
		t.Fatal("Service did not stop Discover")
	}
	rd := redis.GetRedis(registry, nil)
	if rd.Do("HLEN", app).Int() != 0 {
		t.Fatal("Service shutdown left a registration")
	}
}

// Integration fixtures must have membership and a lease in the registry: a
// process-local SetNode alone is intentionally superseded by reconciliation.
func registerDiscoverFixture(t *testing.T, registry, app, addr string) {
	t.Helper()
	rd := redis.GetRedis(registry, nil)
	if rd.Do("EVAL", `redis.call("HSET",KEYS[1],ARGV[1],100);redis.call("SETEX",KEYS[2],30,"1");redis.call("PUBLISH","CH_"..KEYS[1],ARGV[1].." 100");return 1`, 2, app, app+"_"+addr, addr).Error != nil {
		t.Fatal("cannot register discovery fixture")
	}
	t.Cleanup(func() { rd.HDEL(app, addr); rd.DEL(app + "_" + addr); discover.SetNode(app, addr, 0) })
	rd.PUBLISH("CH_"+app, addr+" 100")
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for discover.GetAppNodes(app)[addr] == nil {
		select {
		case <-timeout.C:
			t.Fatalf("fixture discovery timed out: registry=%s calls=%v nodes=%v hash=%v lease=%v", discover.Config.Registry, discover.Config.Calls, discover.GetAppNodes(app), rd.HGETALL(app), rd.EXISTS(app+"_"+addr))
		case <-tick.C:
		}
	}
}

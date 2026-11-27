package webadmin

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"robot/internal/foundation/config"
)

func TestHandleCallPreservesSlowRobotResultPastHTTPWriteTimeout(t *testing.T) {
	for _, ok := range []bool{true, false} {
		t.Run(fmt.Sprint(ok), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			var calls atomic.Int32
			backendDone := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					backendDone <- err
					return
				}
				defer conn.Close()
				calls.Add(1)
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				request := make([]byte, 1024)
				_, err = conn.Read(request)
				if err == nil {
					time.Sleep(80 * time.Millisecond)
					_, err = fmt.Fprintf(conn, `<tw><result>{"ok":%t,"error":"restart failed","result":{"synced":2}}</result></tw>`, ok)
				}
				backendDone <- err
			}()
			s := New(&config.SysConfig{}, listener.Addr().String(), "")
			server := httptest.NewUnstartedServer(s.withDiagnostics(http.HandlerFunc(s.handleCall)))
			server.Config.WriteTimeout = 20 * time.Millisecond
			server.Start()
			defer server.Close()
			client := server.Client()
			client.Timeout = 3 * time.Second
			response, err := client.Post(server.URL, "application/json", strings.NewReader(`{"command":"marketSyncItemInfo","payload":{}}`))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			data, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("decode result: %v; body=%s", err, data)
			}
			if result.OK != ok || (!ok && result.Error != "restart failed") {
				t.Fatalf("robot result lost: %s", data)
			}
			if err := <-backendDone; err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatalf("backend executed %d times", calls.Load())
			}
		})
	}
}

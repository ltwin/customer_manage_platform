package creativeops_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// This proxy drops exactly one real PostgreSQL COMMIT acknowledgement after the
// database sent CommandComplete. It tests the ambiguous commit boundary without
// a fake transaction or a production-only testing hook.
func loseCommitAcknowledgement(t *testing.T, databaseURL string) (string, *atomic.Bool) {
	t.Helper()
	u, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	target := u.Host
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var dropped atomic.Bool
	var wg sync.WaitGroup
	var mu sync.Mutex
	connections := make(map[net.Conn]bool)
	wg.Go(func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			upstream, err := net.Dial("tcp", target)
			if err != nil {
				_ = client.Close()
				continue
			}
			mu.Lock()
			connections[client] = true
			connections[upstream] = true
			mu.Unlock()
			wg.Go(func() {
				defer func() {
					_ = client.Close()
					_ = upstream.Close()
					mu.Lock()
					delete(connections, client)
					delete(connections, upstream)
					mu.Unlock()
				}()
				copied := make(chan struct{})
				go func() { defer close(copied); _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
				defer func() { _ = client.Close(); _ = upstream.Close(); <-copied }()
				for {
					var header [5]byte
					if _, err := io.ReadFull(upstream, header[:]); err != nil {
						return
					}
					length := int(binary.BigEndian.Uint32(header[1:])) - 4
					if length < 0 || length > 16<<20 {
						return
					}
					body := make([]byte, length)
					if _, err := io.ReadFull(upstream, body); err != nil {
						return
					}
					if header[0] == 'C' && string(body) == "COMMIT\x00" && dropped.CompareAndSwap(false, true) {
						return
					}
					if _, err := client.Write(header[:]); err != nil {
						return
					}
					if _, err := client.Write(body); err != nil {
						return
					}
				}
			})
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		for c := range connections {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	u.Host = listener.Addr().String()
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String(), &dropped
}

func TestLostCommitAckReturnsUnknownAndReplaysDurableReceipt(t *testing.T) {
	f := setup(t)
	proxy, dropped := loseCommitAcknowledgement(t, f.url)
	db, err := store.Open(context.Background(), proxy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	scope := db.ScopeFor(auth.AccountContext{AccountID: "creative-a"})
	cmd := command("lost-ack")
	exec := creativeops.Executor{}
	receipt, err := exec.Run(context.Background(), scope, operation(), cmd)
	if !dropped.Load() || !errors.Is(err, store.ErrCommitOutcomeUnknown) || receipt.OperationID != "" {
		t.Fatalf("receipt=%+v dropped=%v error=%v", receipt, dropped.Load(), err)
	}
	// A fresh direct connection sees the exact committed result; replay cannot run Apply twice.
	if _, err := exec.Lookup(context.Background(), f.a, cmd.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Run(context.Background(), f.a, operation(), cmd); err != nil {
		t.Fatal(err)
	}
	if count(t, f, "creative_test_effects") != 1 {
		t.Fatal("commit recovery repeated the effect")
	}
}
